package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/pquerna/otp/totp"
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
		@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;500;600;700&display=swap');
		:root { --primary: #10b981; --primary-dark: #059669; --bg: #f1f5f9; --card-bg: #ffffff; --text-main: #1e293b; --text-muted: #64748b; }
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: var(--bg); color: var(--text-main); display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 20px; }
		.card { background: var(--card-bg); padding: 40px; border-radius: 32px; box-shadow: 0 20px 50px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 480px; animation: slideUp 0.5s ease-out; }
		@keyframes slideUp { from { opacity: 0; transform: translateY(20px); } to { opacity: 1; transform: translateY(0); } }
		.btn { cursor: pointer; border: none; border-radius: 16px; font-weight: 700; padding: 16px; background: linear-gradient(135deg, var(--primary), var(--primary-dark)); color: white; width: 100%; display: block; text-decoration: none; margin-top: 15px; transition: all 0.3s ease; box-shadow: 0 4px 12px rgba(16, 185, 129, 0.2); font-size: 16px; text-align: center; }
		.btn:hover { transform: translateY(-2px); box-shadow: 0 6px 20px rgba(16, 185, 129, 0.3); }
		.form-input { font-size: 16px; width: 100%; padding: 14px; border: 2px solid #e2e8f0; border-radius: 16px; margin: 8px 0 16px 0; outline: none; box-sizing: border-box; font-family: inherit; }
		.appointment-card { background: #f8fafc; padding: 18px; margin-bottom: 12px; border-radius: 20px; text-align: left; position: relative; border: 1px solid #f1f5f9; transition: 0.2s; }
		.appointment-card:hover { border-color: var(--primary); background: #fff; }
		.btn-delete { position: absolute; right: 15px; top: 15px; color: #94a3b8; text-decoration: none; font-size: 18px; font-weight: bold; }
		label { display: block; text-align: left; font-size: 13px; font-weight: 600; color: var(--text-muted); margin-left: 5px; }
		hr { border: 0; border-top: 1px solid #f1f5f9; margin: 30px 0; }
		#search-input { background: #f8fafc; border-radius: 12px; padding: 10px 15px; margin-bottom: 20px; border: 1px solid #cbd5e1; }
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

func sendEmailOTP(toEmail, code string) {
	from, pass := os.Getenv("EMAIL_USER"), os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	subject := fmt.Sprintf("Subject: Code %s (%d)\r\n\r\n", code, time.Now().Unix())
	msg := []byte(subject + "HealthTech verification code: " + code)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, msg)
}

func sendAppointmentEmail(toEmail, doctor, dateStr string) {
	from, pass := os.Getenv("EMAIL_USER"), os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	t, _ := time.Parse("2006-01-02T15:04", dateStr)
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	subject := "Subject: Запись подтверждена - HealthTech\r\n"
	mime := "MIME-version: 1.0; Content-Type: text/html; charset=\"UTF-8\";\r\n\r\n"
	body := fmt.Sprintf("<div style='font-family:sans-serif;padding:20px;'><h2 style='color:#10b981;'>🌿 Вы записаны!</h2><p>Врач: <b>%s</b><br>Время: <b>%s</b></p></div>", doctor, t.Format("02.01.2006 в 15:04"))
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, []byte(subject+mime+body))
}

func main() {
	initDB()

	// 1. HOME
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		session, err := r.Cookie("session_valid")
		if err == nil && session.Value == "true" {
			http.Redirect(w, r, "/dashboard", 302)
			return
		}
		fmt.Fprintf(w, "<html><head><meta name='viewport' content='width=device-width, initial-scale=1'>%s</head><body><div class='card'><h1>🌿 HealthTech</h1><a href='%s' class='btn'>Войти через Google</a></div></body></html>", sharedStyles, googleOAuthConfig.AuthCodeURL("state"))
	})

	// 2. CALLBACK
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, _ := googleOAuthConfig.Exchange(context.Background(), code)
		client := googleOAuthConfig.Client(context.Background(), token)
		resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		var userInfo struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&userInfo)
		http.SetCookie(w, &http.Cookie{Name: "user_email", Value: userInfo.Email, Path: "/", MaxAge: 86400, HttpOnly: true})
		http.Redirect(w, r, "/otp-verify", 302)
	})

	// 3. OTP VERIFY (Стабильный ключ)
	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("user_email")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}
		email := cookie.Value

		var secret string
		err = db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND doctor_name = 'System' LIMIT 1", email).Scan(&secret)
		if err != nil {
			key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
			secret = key.Secret()
			db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'User', '2026-01-01', 'System')", email, secret)
		}

		otpCode, _ := totp.GenerateCode(secret, time.Now())
		fmt.Printf("LOGIN: %s -> %s\n", email, otpCode)
		go sendEmailOTP(email, otpCode)
		http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("user_email")
			if err != nil {
				http.Redirect(w, r, "/", 302)
				return
			}

			var secret string
			db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND doctor_name = 'System' LIMIT 1", cookie.Value).Scan(&secret)

			// МЫ УВЕЛИЧИВАЕМ Skew до 20.
			// Это значит, что сервер проверит коды на 10 минут назад и на 10 минут вперед.
			valid, _ := totp.ValidateCustom(strings.TrimSpace(r.FormValue("code")), strings.TrimSpace(secret), time.Now(), totp.ValidateOpts{
				Skew:   20,
				Digits: 6,
				Period: 30,
			})

			if valid {
				http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "true", Path: "/", MaxAge: 86400, HttpOnly: true})
				http.Redirect(w, r, "/dashboard", 302)
			} else {
				// Добавим отладочный вывод в консоль Render, чтобы видеть, что происходит
				fmt.Printf("ERROR: Неверный код %s для пользователя %s\n", r.FormValue("code"), cookie.Value)
				fmt.Fprintf(w, "<html><body style='font-family:sans-serif; text-align:center; padding:50px;'><h2>Ошибка кода</h2><p>Сервер не принял код. Убедитесь, что вы вводите код из САМОГО ПОСЛЕДНЕГО письма.</p><a href='/otp-verify'>Попробовать еще раз</a></body></html>")
			}
		})
		valid, _ := totp.ValidateCustom(strings.TrimSpace(r.FormValue("code")), strings.TrimSpace(secret), time.Now(), totp.ValidateOpts{Skew: 10})
		if valid {
			http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "true", Path: "/", MaxAge: 86400, HttpOnly: true})
			http.Redirect(w, r, "/dashboard", 302)
		} else {
			fmt.Fprintf(w, "<script>alert('Неверно! Проверьте время на телефоне.'); window.history.back();</script>")
		}
	})

	// 5. DASHBOARD (Дизайн + Поиск)
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		emailCookie, errEmail := r.Cookie("user_email")
		sessionCookie, errSess := r.Cookie("session_valid")
		if errEmail != nil || errSess != nil || sessionCookie.Value != "true" {
			http.Redirect(w, r, "/", 302)
			return
		}

		rows, _ := db.Query("SELECT id, doctor_name, appointment_date FROM appointments WHERE user_email = $1 AND doctor_name != 'System' ORDER BY appointment_date ASC", emailCookie.Value)
		var listHTML string
		for rows.Next() {
			var id int
			var d, dt string
			rows.Scan(&id, &d, &dt)
			t, _ := time.Parse("2006-01-02T15:04", dt)
			listHTML += fmt.Sprintf(`<div class="appointment-card" data-doctor="%s"><a href="/delete?id=%d" class="btn-delete" onclick="return confirm('Отменить?')">✕</a><strong>%s</strong><br><small>📅 %s</small></div>`, d, id, d, t.Format("02.01.2006 15:04"))
		}

		fmt.Fprintf(w, `<html><head><meta charset='UTF-8'><meta name='viewport' content='width=device-width, initial-scale=1'>%s</head>
		<body><div class='card'><h2>Мой кабинет</h2><p style='font-size:12px;color:var(--text-muted);'>%s</p>
		<input type="text" id="search-input" class="form-input" placeholder="🔍 Поиск врача..." onkeyup="filter()">
		<div id="list" style="margin-top:20px;">%s</div><hr>
		<form action='/add' method='POST'><label>Врач</label><input name='doc' class='form-input' required placeholder='Окулист'><label>Дата</label><input type='datetime-local' name='date' class='form-input' required><button class='btn'>Записаться</button></form>
		<br><a href='/logout' style='color:var(--text-muted);text-decoration:none;font-size:13px;'>Выйти</a></div>
		<script>function filter(){let val=document.getElementById('search-input').value.toLowerCase();let cards=document.getElementsByClassName('appointment-card');for(let c of cards){c.style.display=c.getAttribute('data-doctor').toLowerCase().includes(val)?"":"none"}}</script>
		</body></html>`, sharedStyles, emailCookie.Value, listHTML)
	})

	// 6. ADD & DELETE & LOGOUT
	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("user_email")
		doc, date := r.FormValue("doc"), r.FormValue("date")
		if err == nil && doc != "" {
			db.Exec("INSERT INTO appointments (user_email, doctor_name, appointment_date, patient_name, totp_secret) VALUES ($1, $2, $3, 'Patient', '')", cookie.Value, doc, date)
			go sendAppointmentEmail(cookie.Value, doc, date)
		}
		http.Redirect(w, r, "/dashboard", 302)
	})

	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		cookie, err := r.Cookie("user_email")
		if id != "" && err == nil {
			db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, cookie.Value)
		}
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
