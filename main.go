package main

import (
	_ "embed"
	"log"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/fsnotify/fsnotify"
	"github.com/lhhong/ha433pairer/server"
	"github.com/lhhong/ha433pairer/templates"
)

//go:embed css/output.css
var tailwind []byte

func main() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Unexpected error", err)
	}
	defer watcher.Close()

	config := server.InitConfig(watcher)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")
		templates.RootDoc(config.DevConf.Devices).Render(r.Context(), w)
	})
	http.Handle("/add-new-device", templ.Handler(templates.AddDeviceDialog()))
	http.HandleFunc("/add-new-trigger", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		w.Header().Add("Content-Type", "text/html")
		templates.AddTriggerDialog(r.Form.Get("deviceId")).Render(r.Context(), w)
	})
	http.Handle("/empty-dialog", templ.Handler(templates.EmptyDialog()))
	http.HandleFunc("/create-device", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		newDevice, err := server.AddDevice(config, r.Form.Get("name"))
		if err != nil {
			log.Println(err)
			w.WriteHeader(500)
		} else {
			w.Header().Add("Content-Type", "text/html")
			w.Header().Add("HX-Trigger-After-Swap", "closeDialog")
			templates.DeviceEntry(*newDevice).Render(r.Context(), w)
		}
	})
	http.HandleFunc("/create-trigger", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		newTrigger, err := server.AddTrigger(config, r.Form.Get("deviceId"), r.Form.Get("name"))
		if err != nil {
			log.Println(err)
			w.WriteHeader(500)
		} else {
			time.Sleep(3 * time.Second)
			w.Header().Add("Content-Type", "text/html")
			w.Header().Add("HX-Trigger-After-Swap", "closeDialog")
			templates.TriggerEntry(*newTrigger).Render(r.Context(), w)
		}
	})
	http.HandleFunc("/css/output.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/css")
		w.Write(tailwind)
	})
	log.Fatal(http.ListenAndServe(":8943", nil))
}
