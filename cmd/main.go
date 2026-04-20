package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/pquerna/otp/totp" // Прямое использование библиотеки
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
	Endpoint:     google.Endpoint,
}

var db *sql.DB

const sharedStyles = `
	<style>
		:root { --primary: #10b981; --primary-hover: #059669; --bg: #f8fafc; --text: #1e293b; }
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: 'Inter', sans-serif; background: var(--bg); color: var(--text); display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
		.card { background: white; padding: 40px; border-radius: 24px; box-shadow: 0 10px 30px rgba(0,0,0,0.05); border: 1px solid #e2e8f0; text-align: center; width: 100%; max-width: 450px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; transition: all 0.2s; text-decoration: none; display: inline-block; padding: 14px; background: var(--primary); color: white; width: 100%; font-size: 16px; margin-top: 15px; }
		.btn:hover { background: var(--primary-hover); transform: translateY(-1px); }
		input { width: 100%; padding: 14px; border: 2px solid #f1f5f9; border-radius: 12px; font-size: 20px; text-align: center; outline: none; margin-top: 15px; }
		.qr-box { background: #f9fafb; padding: 15px; border-radius: 16px; margin: 20px 0; border: 1px dashed #cbd5e1; }
		.container { width: 100%; max-width: 900px; padding: 20px; align-self: flex-start; }
		table { width: 100%; border-collapse: collapse; margin-top: 20px; }
		th, td { padding: 12px; text-align: left; border-bottom: 1px solid #f1f5f9; }
	</style>
`

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	if err = db.Ping(); err != nil {
		db, _ = sql.Open("postgres", connStr+"?sslmode=require")
	}
}

func getCookie(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func main() {
	initDB()

	// 1. Главная
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if email := getCookie(r, "user_session"); email != "" {
			http.Redirect(w, r, "/dashboard", 302)
			return
		}
		url := googleOAuthConfig.AuthCodeURL("state", oauth2.SetAuthURLParam("prompt", "select_account"))
		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
			<div class="card">
				<div style="font-size:50px;">🌿</div>
				<h1>HealthTech Pro</h1>
				<p style="color:#64748b;">Вход с OTP защитой</p>
				<a href="%s" class="btn">Войти через Google</a>
			</div>
		</body></html>`, sharedStyles, url)
	})

	// 2. Callback Google
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}

		client := googleOAuthConfig.Client(context.Background(), token)
		resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		var userInfo struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&userInfo)

		http.SetCookie(w, &http.Cookie{
			Name: "otp_pending", Value: userInfo.Email, Path: "/", Expires: time.Now().Add(5 * time.Minute), HttpOnly: true,
		})
		http.Redirect(w, r, "/otp-verify", 302)
	})

	// 3. Страница OTP (Генерация QR через TOTP)
	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		email := getCookie(r, "otp_pending")
		if email == "" {
			http.Redirect(w, r, "/", 302)
			return
		}

		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND totp_secret != '' LIMIT 1", email).Scan(&secret)

		if secret == "" {
			// Генерируем новый секрет
			key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
			secret = key.Secret()
			db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'System', '2026-01-01', 'Admin')", email, secret)

			qrUrl := fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=180x180&data=otpauth://totp/HealthTech:%s?secret=%s&issuer=HealthTech", email, secret)

			fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
				<div class="card">
					<h1>Защита аккаунта</h1>
					<p>Отсканируйте код в приложении:</p>
					<div class="qr-box"><img src="%s"></div>
					<form action="/otp-check" method="POST">
						<input type="text" name="code" placeholder="000000" maxlength="6" required autofocus>
						<button type="submit" class="btn">Активировать</button>
					</form>
				</div>
			</body></html>`, sharedStyles, qrUrl)
			return
		}

		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
			<div class="card">
				<h1>OTP Проверка</h1>
				<p>Введите код из телефона:</p>
				<form action="/otp-check" method="POST">
					<input type="text" name="code" placeholder="000000" maxlength="6" required autofocus>
					<button type="submit" class="btn">Войти</button>
				</form>
			</div>
		</body></html>`, sharedStyles)
	})

	// 4. Проверка OTP через TOTP Validate
	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		email := getCookie(r, "otp_pending")
		userCode := r.FormValue("code")

		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND totp_secret != '' LIMIT 1", email).Scan(&secret)

		// ПРЯМОЕ ИСПОЛЬЗОВАНИЕ TOTP
		if totp.Validate(userCode, secret) {
			http.SetCookie(w, &http.Cookie{
				Name: "user_session", Value: email, Path: "/", Expires: time.Now().Add(30 * 24 * time.Hour), HttpOnly: true,
			})
			http.Redirect(w, r, "/dashboard", 302)
		} else {
			fmt.Fprintf(w, `<script>alert("Неверный код!"); window.history.back();</script>`)
		}
	})

	// 5. Дашборд
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		email := getCookie(r, "user_session")
		if email == "" {
			http.Redirect(w, r, "/", 302)
			return
		}

		rows, _ := db.Query("SELECT id, patient_name, appointment_date, doctor_name FROM appointments WHERE user_email = $1 AND patient_name != 'System' ORDER BY id DESC", email)
		defer rows.Close()

		var tableHtml string
		for rows.Next() {
			var id int
			var pName, pDate, pDoc string
			rows.Scan(&id, &pName, &pDate, &pDoc)
			tableHtml += fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td><td><a href="/delete?id=%d">🗑️</a></td></tr>`, pName, pDate, pDoc, id)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body style="display:block;"><div class="container">
			<div class="card" style="max-width:100%%; text-align:left;">
				<div style="display:flex; justify-content:space-between;">
					<h2>🌿 Журнал: %s</h2>
					<a href="/logout" style="color:red; text-decoration:none;">Выйти</a>
				</div>
				<form action="/save" method="POST" style="margin-top:20px; display:grid; grid-template-columns: 2fr 1fr 1fr 1fr; gap:10px;">
					<input type="text" name="name" placeholder="Пациент" required style="margin:0; text-align:left;">
					<input type="date" name="date" required style="margin:0;">
					<select name="doc" style="padding:12px; border-radius:12px; border:2px solid #f1f5f9;"><option>Терапевт</option><option>Кардиолог</option></select>
					<button type="submit" class="btn" style="margin:0;">OK</button>
				</form>
				<table><thead><tr><th>Имя</th><th>Дата</th><th>Врач</th><th></th></tr></thead><tbody>%s</tbody></table>
			</div>
		</div></body></html>`, sharedStyles, email, tableHtml)
	})

	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		email := getCookie(r, "user_session")
		if email != "" {
			db.Exec("INSERT INTO appointments (patient_name, appointment_date, doctor_name, user_email) VALUES ($1, $2, $3, $4)",
				r.FormValue("name"), r.FormValue("date"), r.FormValue("doc"), email)
		}
		http.Redirect(w, r, "/dashboard", 302)
	})

	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		email := getCookie(r, "user_session")
		id := r.URL.Query().Get("id")
		if email != "" {
			db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, email)
		}
		http.Redirect(w, r, "/dashboard", 302)
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "user_session", Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/", 302)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
