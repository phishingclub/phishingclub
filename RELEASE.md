# Changelog

## [1.17.0] - 2025-11-16
- Added option to to add data to webhook events
- Fix added a couple of webhook calls
- Fix add global rewrite rules to requests without mitm session
- Fix bad handling of brotli/gzip with browser empersonation 
- Fix cookie events captured before all required captures
- Removed unused sorting column

## [1.16.0] - 2025-11-12
- Added synthetic 'email read' event when visiting a lure without having loaded a tracking pixel in a email
- Added {{.FromName}}, {{.FromEmail}} and {{.Subject}} variable support to API sender
- Fix {{.APIKey}} not rendered in API request header

## [1.15.1] - 2025-11-11
- Fix missing meta data field on some events
- Handle unknown events on campaign page

## [1.15.0] - 2025-11-11
- Added tools page with ip geo lookup and JA4 fingerprint builder
- Added option to save additional recipient event data (ja4, Sec-CH-UA-Platform header and Accept-Lang header)
- Deny page visits are now saved as events
- Fix updating geo filter not updating filter

## [1.14.0] - 2025-11-09
- Added allow / deny filtering based on geo IP
- Added support for sock5 with authentication

## [1.13.1] - 2025-11-07
- Fix bad calculation for submitted on campaign page
- Fix ensure folder exists for attachments

## [1.13.0] - 2025-11-07
- Added proxy request JA4 impersonation
- Added JA4 filtering with wildcard support in allow deny lists
- Changed IP filtering to filtering
- Bumped dependencies
- Fixed overly eager proxy auto completion in editor
- Fixed bug in obfuscation that could cause dublicate variables

## [1.12.0] - 2025-11-04
- Added tls directive for proxy domains
- Added self signed certificates for domains
- Added expand mode to SimpleCodeEditor
- Align proxy editor UI with normal editor
- All campaign trendline settings are saved
- Clear proxy session when changing a proxy config
- Fixed unused config field

## [1.11.0] - 2025-11-01
- Added option to use campaign obfuscation
- Removed details/editor and added expand option to editor
- Fix editor preview bug when toggled multiple times

## [1.10.0] - 2025-10-31
- Added release image on ghcr
- Added option to pin menu
- Pagination now disables previous and/or next button in appropriate cases
- Added log scale and relative metrics to Campaign Trendline
- Trendline settings are now saved
- Updated custom company stats table to more than just percentages
- Various UI style fixing mostly related to firefox and tables
- Various fixes to Campaign Trendline
- Fix proxy host rules reacting to other hosts rules
- Fix custom stats not added to completed campaigns on dashboard
- Fix bug with importing data in nested folders
- Fix preview domain always visible in editor
- Fix bug where domains might be shown in editor


## [1.9.1] - 2025-10-25
- Fixed missing proxy logic for modifying sessionless request and headers
- Fixed actions width to align with header width

## [1.9.0] - 2025-10-24
- Revamped proxy access directive
- Added proxy rewrite URL directive
- Added custom stats for company
- Various changes to the proxing logic
- Simplified create campaign modal
- Simplefied create template modal
- Campaign anonymization now requires confirmation
- Improved dashboard campaign trendline
- Fixed response for host specific path matched any host
- Fixed copy button copied wrong text
- Fixed bad dark mode color on copy campaign recipient event
- Fixed check campaign name before step 2 on copy campaign
- Fixed copy campaign transfering values that should be reset
- Fixed bad mapping on campaign templates 'is complete'
- Fixed proxy should not be available in all contexts
- Fixed bug when deleting all assets of a domain
- Added beta tag for Proxy functionality

## [1.8.0] - 2025-10-19
- Campaigns now support Anti-Bot / Evasion page
- Proxy campaign pages now support IP filtering
- Minor UI update / fixes

## [1.7.0] - 2025-10-16
- New DOM engine choice for proxy rewrite directive
- New response proxy directive
- New orhaned recipients page with delete all
- Quick navigation with CTRL+p
- A comment can now be added to a company
- Added confirm alerts to company and shared data export
- When in company context tables show which scope a row belongs to
- Fix panic on missing nil checks of various proxy rules
- Fix panic on export shared view data
- Fix missing validation of type on allow/deny list
- Fix error still shown when updating with shortcut
- Fix campagin box position on trendline

## [1.6.2] - 2025-10-13
- Remove dark mode browser specific styling for date components

## [1.6.1] - 2025-10-13
- Fix proxy domain comparison
- Improve campaign trendline campaign box
- Escape context in analytics graphs
- Fix login page on dark mode

## [1.6.0] - 2025-10-12
- Added debug flag
- Option to install example templates on setup
- Support for CTRL+s to save when updating email, page or domain without closing editing modal
- Many UI updates
- Set as sent now has a confirm modal
- Improve tabbing in form modals
- Fix if first page is a proxy, skip the campaign template domain

## [1.5.0] - 2025-10-08
- Added access control rules for proxys
- Completion help for proxys in editor
- Vim mode for editors
- Fix proxy header rewrite not being done
- Fix company attachments in shared context
- Fix panic on loading tracking pixel for deleted campaign
- Various UI fixes
- Campaigns now default to saving submitted data
- Updated embedded licenses
- Removed securejoin dependency in favor of os.OpenRoot (native)

## [1.4.0] - 2025-09-30
- Added proxy (MITM) functionality
- Added 'Advanced mode' to interactive installer
- Various UI fixes
- Fix Editor style isolation
- Bump dependency

## [1.3.1] - 2025-09-21
- Improved width of links in tables
- Fixed asset page not showing domains
- Fixed domain assets shown under global assets
- Improve asset delete modal text
- Removed asset preview icon background
- Minor improvements to install / login UI

## [1.3.0] - 2025-09-19
- Added dark mode support and various UI improvements
- Added manual backup functionality
- Added reported functionality for phishing campaigns
- Added recipient manual send action
- Added validation on save
- Added link to release information on update modal and page
- Fixed copy campaign wrong text on create
- Fixed HTML to text template handling
- Fixed bad title on settings page
- Fixed dashboard scroll to top issue
- Improved send again texts
- Improved modal error position
- Moved recent campaigns to bottom of dashboard
- Bumped Go version and dependencies

## [1.2.1] - 2025-09-15
- Add debug logging to SMTP
- Fix excessive table URL params
- Bump backend and frontend dependencies
- Add debug log for SMTP

## [1.2.0] - 2025-09-04
- Added support for YmdHis Date and Base64 template functions
- Improved campaign review details
- Fix import modal not scrolling to bottom after import

## [1.1.13] - 2025-08-30
- Fix too many get all sessions params sent
- Fix invalidate all sessions
- Fix missing change company
- Fix improve table checkboxes to Yes/No
- Table menu is now larger and placed more correctly
- Simple code editor for API senders body
- Removed CRM/License link from developer panel

## [1.1.12] - 2025-08 -29
- Added a update button to campaigns details page
- Toggle test campaign on dashboard
- Fix trend legend alignment
- Improve domain TLS certificate management naming
- Campaign creator, sort by and order not optional in delivery
- Smaller height on table rows
- Fix group recipient column headers
- Improve validation error messages
- Campaign details show correct "Data saving" and "anonymization"
- Campaign update handle anonymization and close at

## [1.1.11] - 2025-08-29
- Show full error on invalid password when installing

## [1.1.10] - 2025-08-27
- Removed systemd inline comments
- Version check for updates in development

## [1.1.9] - 2025-08-23
- Fixed db lock bug in installer
- Removed license text in installer
