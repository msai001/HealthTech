package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/smtp"
	"os"
	"strings"
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

const sharedStyles = `
	<style>
		@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;600;700&display=swap');
		:root { --primary: #10b981; --bg: #f1f5f9; --text: #1e293b; }
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: var(--bg); color: var(--text); display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 20px; }
		.card { background: white; padding: 40px; border-radius: 30px; box-shadow: 0 10px 30px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 400px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 16px; background: var(--primary); color: white; width: 100%; display: block; text-decoration: none; margin-top: 15px; }
		.form-input { font-size: 18px; width: 100%; padding: 12px; border: 2px solid #e2e8f0; border-radius: 12px; margin: 10px 0; text-align: center; box-sizing: border-box; }
		.item { background: #f8fafc; padding: 15px; border-radius: 15px; margin-bottom: 10px; text-align: left; position: relative; }
		.del { position: absolute; right: 15px; top: 15px; color: #94a3b8; text-decoration: none; font-weight: bold; }
	</style>
`

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
}

func sendMail(to, subject, body string) {
	from, pass := os.Getenv("EMAIL_USER"), os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	msg := []byte("Subject: " + subject + "\r\n\r\n" + body)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{to}, msg)
}

func main() {
	initDB()
	rand.Seed(time.Now().UnixNano())

	// ГЛАВНАЯ
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session_valid"); err == nil && c.Value == "true" {
			http.Redirect(w, r, "/dashboard", 302)
			return
		}
		fmt.Fprintf(w, "<html><head>%s</head><body><div class='card'><h1>🌿 HealthTech</h1><a href='%s' class='btn'>Войти через Google</a></div></body></html>", sharedStyles, googleOAuthConfig.AuthCodeURL("state"))
	})

	// CALLBACK GOOGLE
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, _ := googleOAuthConfig.Exchange(context.Background(), code)
		client := googleOAuthConfig.Client(context.Background(), token)
		resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		var user struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&user)
		http.SetCookie(w, &http.Cookie{Name: "user_email", Value: user.Email, Path: "/", MaxAge: 86400})
		http.Redirect(w, r, "/otp-verify", 302)
	})

	// ГЕНЕРАЦИЯ КОДА
	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("user_email")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}

		code := fmt.Sprintf("%06d", rand.Intn(1000000))
		db.Exec("DELETE FROM appointments WHERE user_email = $1 AND doctor_name = 'System'", c.Value)
		db.Exec("INSERT INTO appointments (user_email, totp_secret, doctor_name, patient_name, appointment_date) VALUES ($1, $2, 'System', 'User', '2026-01-01')", c.Value, code)

		go sendMail(c.Value, "HealthTech Code", "Ваш код для входа: "+code)
		fmt.Fprintf(w, "<html><head>%s</head><body><div class='card'><h2>Введите код</h2><form action='/otp-check' method='POST'><input name='code' class='form-input' required><button class='btn'>Войти</button></form></div></body></html>", sharedStyles)
	})

	// ПРОВЕРКА КОДА
	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("user_email")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}

		var saved string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND doctor_name = 'System' ORDER BY id DESC LIMIT 1", c.Value).Scan(&saved)

		if strings.TrimSpace(r.FormValue("code")) == saved && saved != "" {
			http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "true", Path: "/", MaxAge: 86400})
			http.Redirect(w, r, "/dashboard", 302)
		} else {
			fmt.Fprintf(w, "<script>alert('Ошибка кода!'); window.history.back();</script>")
		}
	})

	// КАБИНЕТ
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		cEmail, errE := r.Cookie("user_email")
		cSess, errS := r.Cookie("session_valid")
		if errE != nil || errS != nil || cSess.Value != "true" {
			http.Redirect(w, r, "/", 302)
			return
		}

		rows, _ := db.Query("SELECT id, doctor_name, appointment_date FROM appointments WHERE user_email = $1 AND doctor_name != 'System' ORDER BY id DESC", cEmail.Value)
		var list string
		for rows.Next() {
			var id int
			var d, dt string
			rows.Scan(&id, &d, &dt)
			list += fmt.Sprintf(`<div class="item"><a href="/delete?id=%d" class="del">✕</a><strong>%s</strong><br><small>%s</small></div>`, id, d, dt)
		}

		fmt.Fprintf(w, "<html><head>%s</head><body><div class='card'><h2>Кабинет</h2><p>%s</p>%s<hr><form action='/add' method='POST'><input name='doc' class='form-input' placeholder='Врач' required><input type='datetime-local' name='date' class='form-input' required><button class='btn'>Записаться</button></form><br><a href='/logout'>Выход</a></div></body></html>", sharedStyles, cEmail.Value, list)
	})

	// ДОБАВИТЬ / УДАЛИТЬ / ВЫХОД
	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("user_email")
		db.Exec("INSERT INTO appointments (user_email, doctor_name, appointment_date, patient_name, totp_secret) VALUES ($1, $2, $3, 'Patient', '')", c.Value, r.FormValue("doc"), r.FormValue("date"))
		http.Redirect(w, r, "/dashboard", 302)
	})

	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		c, _ := r.Cookie("user_email")
		db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, c.Value)
		http.Redirect(w, r, "/dashboard", 302)
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/", 302)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
