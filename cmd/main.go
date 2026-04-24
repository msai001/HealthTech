package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"openid", "email", "profile"},
	Endpoint:     google.Endpoint,
}

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	rand.Seed(time.Now().UnixNano())

	http.HandleFunc("/api/auth/google", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})

	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/api/data", handleGetData) // API для таблицы
	http.HandleFunc("/", handleRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting v1.6.2")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	token, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	client := googleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var user struct{ Email string }
	json.NewDecoder(resp.Body).Decode(&user)

	otp := fmt.Sprintf("%06d", rand.Intn(1000000))
	db.Exec("INSERT INTO appointments (user_email, totp_secret, doctor_name, appointment_date, patient_name) VALUES ($1, $2, 'System Auth', NOW(), 'User Access')", user.Email, otp)

	http.SetCookie(w, &http.Cookie{Name: "user_email", Value: user.Email, Path: "/", MaxAge: 86400})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleGetData(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, doctor_name, appointment_date, patient_name, totp_secret FROM appointments ORDER BY id DESC")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id int
		var doc, date, pat, otp string
		rows.Scan(&id, &doc, &date, &pat, &otp)
		results = append(results, map[string]interface{}{
			"id": id, "doctor": doc, "date": date, "patient": pat, "otp": otp,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<!DOCTYPE html>
		<html>
		<head>
			<title>Health Monitoring Dashboard</title>
			<style>
				body { font-family: sans-serif; background: #f4f7f9; padding: 20px; }
				.card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); max-width: 900px; margin: 0 auto; }
				table { width: 100%; border-collapse: collapse; margin-top: 20px; }
				th, td { padding: 12px; border: 1px solid #eee; text-align: left; }
				th { background: #4285F4; color: white; }
				.btn { background: #4285F4; color: white; padding: 10px 20px; text-decoration: none; border-radius: 4px; display: inline-block; }
			</style>
		</head>
		<body>
			<div class="card">
				<h1>Health Monitoring Dashboard</h1>
				<a href="/api/auth/google" class="btn">Обновить вход через Google</a>
				<table id="data-table">
					<thead>
						<tr>
							<th>ID</th>
							<th>Событие/Врач</th>
							<th>Дата</th>
							<th>Пациент</th>
							<th>Код (OTP)</th>
						</tr>
					</thead>
					<tbody id="content"></tbody>
				</table>
				<p style="color:gray; font-size: 0.8em; margin-top:20px;">Версия системы: 1.6.2 | База данных: PostgreSQL</p>
			</div>
			<script>
				fetch('/api/data')
					.then(res => res.json())
					.then(data => {
						const html = data.map(row => '<tr><td>'+row.id+'</td><td>'+row.doctor+'</td><td>'+row.date+'</td><td>'+row.patient+'</td><td><b>'+row.otp+'</b></td></tr>').join('');
						document.getElementById('content').innerHTML = html;
					});
			</script>
		</body>
		</html>
	`)
}
