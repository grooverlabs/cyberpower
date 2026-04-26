package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"cyberpower/assets"
	"cyberpower/gateways"
	"cyberpower/views"

	"github.com/a-h/templ"
	"github.com/go-fuego/fuego"
)

// registerWebRoutes hangs the HTML dashboard, partials, and form-POST
// handlers off the Fuego server. The JSON API is registered separately
// under /api/ in main().
func registerWebRoutes(s *fuego.Server) {
	// Static assets come from the assets package's embedded FS — same
	// pattern as Triton, so main and any future tests share the bytes.
	fuego.GetStd(s, "/static/", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/static/", http.FileServer(http.FS(assets.Static()))).ServeHTTP(w, r)
	})

	fuego.GetStd(s, "/", handleDashboard)
	fuego.GetStd(s, "/partials/devices", handleDevicesPartial)
	fuego.GetStd(s, "/device/{serial}", handleDeviceDetail)
	fuego.GetStd(s, "/partials/device/{serial}", handleDevicePartial)
	fuego.PostStd(s, "/device/{serial}/battery-test", handleBatteryTestForm)
	fuego.PostStd(s, "/device/{serial}/beeper", handleBeeperForm)
	fuego.PostStd(s, "/alerts/test", handleAlertsTestForm)
}

// sortedSnapshots returns cache snapshots sorted by serial for stable
// rendering across refreshes.
func sortedSnapshots() []gateways.CachedStatus {
	snaps := gateway.Snapshots()
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].Properties.SerialNumber < snaps[j].Properties.SerialNumber
	})
	return snaps
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	flash := r.URL.Query().Get("flash")
	render(w, r, views.Dashboard(sortedSnapshots(), flash, gateway.Notifier().Enabled()))
}

func handleDevicesPartial(w http.ResponseWriter, r *http.Request) {
	render(w, r, views.DeviceList(sortedSnapshots()))
}

func handleDeviceDetail(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	snap, err := gateway.Get(serial)
	if err != nil {
		http.Error(w, "UPS not found", http.StatusNotFound)
		return
	}
	flash := r.URL.Query().Get("flash")
	render(w, r, views.Device(snap, flash))
}

func handleDevicePartial(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	snap, err := gateway.Get(serial)
	if err != nil {
		http.Error(w, "UPS not found", http.StatusNotFound)
		return
	}
	render(w, r, views.DeviceBody(snap))
}

func handleBatteryTestForm(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	action := gateways.BatteryTestAction(r.FormValue("action"))
	flash := "Battery test command sent: " + string(action)
	if err := gateway.RunBatteryTest(serial, action); err != nil {
		flash = "Error: " + err.Error()
	}
	redirectWithFlash(w, r, "/device/"+serial, flash)
}

func handleBeeperForm(w http.ResponseWriter, r *http.Request) {
	serial := r.PathValue("serial")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	enable := r.FormValue("enable") == "true"
	flash := "Beeper disabled"
	if enable {
		flash = "Beeper enabled"
	}
	if err := gateway.SetBeeper(serial, enable); err != nil {
		flash = "Error: " + err.Error()
	}
	redirectWithFlash(w, r, "/device/"+serial, flash)
}

func handleAlertsTestForm(w http.ResponseWriter, r *http.Request) {
	flash := "Test SMS sent — check your phone."
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := gateway.Notifier().TestSend(ctx); err != nil {
		flash = "Error: " + err.Error()
	}
	redirectWithFlash(w, r, "/", flash)
}

// render writes a templ component as the HTTP response.
func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		log.Printf("render: %v", err)
	}
}

// redirectWithFlash sends a 303 to base?flash=... so the form POST
// becomes a GET (PRG pattern) and the user sees a confirmation message.
func redirectWithFlash(w http.ResponseWriter, r *http.Request, base, flash string) {
	q := url.Values{"flash": {flash}}
	http.Redirect(w, r, base+"?"+q.Encode(), http.StatusSeeOther)
}
