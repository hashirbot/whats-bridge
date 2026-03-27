package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"whatsbridge/internal/api"
	"whatsbridge/internal/auth"
	"whatsbridge/internal/bot"
	"whatsbridge/internal/db"
)

func main() {
	log.Println("Starting WhatsBridge by ERPAGENT...")

	// ---- MySQL (usage metrics & scheduled messages) ----
	mysqlDSN := os.Getenv("MYSQL_DSN")
	if mysqlDSN == "" {
		log.Fatal("MYSQL_DSN environment variable is required (e.g. user:pass@tcp(host:3306)/dbname)")
	}
	db.InitDB(mysqlDSN)

	// ---- Seed default admin user ----
	auth.InitUsers()

	// ---- WhatsApp Bot (uses PostgreSQL for session store) ----
	go bot.InitWhatsApp()

	// ---- Background services ----
	go bot.StartSchedulerLoop()
	bot.StartInternetMonitor()

	// ---- HTTP Server ----
	mux := http.NewServeMux()

	// Auth API (no protection needed)
	mux.HandleFunc("/api/auth/login", auth.LoginHandler)
	mux.HandleFunc("/api/auth/logout", auth.AuthLogoutHandler)
	mux.HandleFunc("/api/auth/check", auth.CheckAuthHandler)

	// External API endpoints — NO auth (Laravel connects directly)
	mux.HandleFunc("/api/status", api.StatusHandler)
	mux.HandleFunc("/api/send", api.SendHandler)
	mux.HandleFunc("/api/metrics", api.MetricsHandler)
	mux.HandleFunc("/api/schedule", api.ScheduleHandler)
	mux.HandleFunc("/api/bulk-send", api.BulkSendHandler)
	mux.HandleFunc("/api/qr", api.QRHandler)
	mux.HandleFunc("/api/logout", api.LogoutHandler)
	mux.HandleFunc("/api/connect", api.ConnectHandler)
	mux.HandleFunc("/api/disconnect", api.DisconnectHandler)

	// WebSocket bridge — NO auth (Laravel WSS)
	mux.HandleFunc("/ws/bridge", bot.HandleBridgeWebSocket)

	// Login page (no auth)
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "public/login.html")
	})

	// Protected pages
	mux.HandleFunc("/connect", auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "public/connect.html")
	}))
	mux.HandleFunc("/messages", auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "public/messages.html")
	}))
	mux.HandleFunc("/chat", auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "public/chat.html")
	}))

	// Static assets (no auth for JS/CSS)
	fs := http.FileServer(http.Dir("public"))
	mux.Handle("/js/", fs)

	// Dashboard (protected) + fallback
	mux.HandleFunc("/", auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.ServeFile(w, r, "public/index.html")
			return
		}
		fs.ServeHTTP(w, r)
	}))

	// Koyeb injects PORT; WEB_PORT is a manual override
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("WEB_PORT")
	}
	if port == "" {
		port = "8000"
	}

	fmt.Printf("WhatsBridge dashboard running on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}