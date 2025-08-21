package controller

import (
	"image/png"
	"net/http"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/service"
)

// QRCodeRequest is the request to generate a QR code from a TOTP URL
type QRCodeRequest struct {
	URL     string `json:"url"`
	DotSize int    `json:"dotSize"`
}

// QRGenerator is the QR controller
type QRGenerator struct {
	Common
}

// QRGenerator creates a HTML QR code
// It is returned in an JSON response
func (q *QRGenerator) ToHTML(g *gin.Context) {
	_, _, ok := q.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req QRCodeRequest
	if ok := q.handleParseRequest(g, &req); !ok {
		return
	}
	// generate QR code
	qrCodeBuf, err := service.GenerateQRCode(req.URL, req.DotSize)
	if err != nil {
		q.Logger.Debugw("failed to genereate QR code",
			"error", err,
		)
		q.Response.ServerError(g)
		return
	}
	q.Response.OK(g, qrCodeBuf)
}

// ToTOTPURL generates a QR code from a TOTP URL
func (q *QRGenerator) ToTOTPURL(g *gin.Context) {
	_, _, ok := q.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req QRCodeRequest
	if ok := q.handleParseRequest(g, &req); !ok {
		return
	}
	// generate QR code
	qrCode, err := qr.Encode(
		req.URL,
		qr.M,
		qr.Auto,
	)
	if err != nil {
		q.Logger.Debugw("failed to generate QR code",
			"error", err,
		)
		q.Response.ServerError(g)
		return
	}
	qrCode, err = barcode.Scale(qrCode, 200, 200)
	if err != nil {
		q.Logger.Debugw("failed to scale QR code",
			"error", err,
		)
		q.Response.ServerError(g)
		return
	}
	// output QR code as png
	g.Writer.Header().Set("Content-Type", "image/png")
	err = png.Encode(g.Writer, qrCode)
	if err == nil {
		q.Logger.Debugw("failed to encode QR code",
			"error", err,
		)
		q.Response.ServerError(g)
		return
	}
	// respond
	g.Status(http.StatusOK)
}
