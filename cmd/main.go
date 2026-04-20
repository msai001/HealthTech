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
		:root { --primary: #10b981; --primary-hover: #059669; --danger: #ef4444; --bg: #f8fafc; --text: #1e293b; }
		* { box-sizing: border-box; margin: 0; padding: 0; }
		body { font-family: 'Inter', sans-serif; background: var(--bg); color: var(--text); }
		.container { max-width: 900px; margin: 40px auto; padding: 0 20px; }
		.card { background: white; padding: 30px; border-radius: 20px; box-shadow: 0 10px 25px rgba(0,0,0,0.03); margin-bottom: 25px; border: 1px solid #e2e8f0; }
		h1 { font-size: 24px; font-weight: 800; color: #0f172a; margin-bottom: 20px; }
		.user-nav { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
		.email-badge { background: #dcfce7; color: #166534; padding: 5px 15px; border-radius: 50px; font-size: 13px; font-weight: 600; }
		.btn { cursor: pointer; border: none; border-radius: 10px; font-weight: 700; transition: all 0.2s; text-decoration: none; display: inline-flex; align-items: center; justify-content: center; }
		.btn-primary { background: var(--primary); color: white; width: 100%; padding: 14px; font-size: 16px; }
		.btn-primary:hover { background: var(--primary-hover); transform: translateY(-1px); }
		.btn-delete { background: #fee2e2; color: var(--danger); padding: 8px; font-size: 12px; }
		.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; margin-bottom: 20px; }
		.form-group { text-align: left; margin-bottom: 15px; }
		label { display: block; margin-bottom: 6px; font-size: 13px; font-weight: 600; color: #64748b; }
		input, select { width: 100%; padding: 12px; border: 1px solid #e2e8f0; border-radius: 10px; font-size: 15px; }
		table { width: 100%; border-collapse: collapse; }
		th { text-align: left; padding: 12px; font-size: 12px; color: #94a3b8; text-transform: uppercase; border-bottom: 2px solid #f1f5f9; }
		td { padding: 16px 12px; border-bottom: 1px solid #f1f5f9; font-size: 14px; }
		.doctor-tag { background: #f1f5f9; color: #475569; padding: 3px 8px; border-radius: 6px; font-size: 12px; font-weight: 600; }
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

// Функция для получения email из куки
func getEmailFromCookie(r *http.Request) string {
	cookie, err := r.Cookie("user_session")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func main() {
	initDB()

	// Главная страница
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Если кука уже есть, сразу кидаем в кабинет
		if email := getEmailFromCookie(r); email != "" {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}

		url := googleOAuthConfig.AuthCodeURL("state")
		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
			<div class="container" style="text-align:center; margin-top: 100px;">
				<div class="card">
					<div style="font-size:48px;">🌿</div>
					<h1>HealthTech Pro</h1>
					<p style="color:#64748b; margin-bottom:30px;">Профессиональное управление записями</p>
					<a href="%s" class="btn btn-primary">Войти через Google</a>
				</div>
			</div>
		</body></html>`, sharedStyles, url)
	})

	// Callback после входа
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

		// СОХРАНЯЕМ ТОКЕН (Email) в КУКИ на 30 дней
		http.SetCookie(w, &http.Cookie{
			Name:     "user_session",
			Value:    userInfo.Email,
			Path:     "/",
			Expires:  time.Now().Add(30 * 24 * time.Hour),
			HttpOnly: true,
		})

		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

	// Личный кабинет
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		email := getEmailFromCookie(r)
		if email == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		rows, _ := db.Query("SELECT id, patient_name, appointment_date, doctor_name FROM appointments WHERE user_email = $1 ORDER BY id DESC", email)
		defer rows.Close()

		var rowsHtml string
		for rows.Next() {
			var id int
			var pName, pDate, pDoc string
			rows.Scan(&id, &pName, &pDate, &pDoc)
			rowsHtml += fmt.Sprintf(`
				<tr>
					<td><b>%s</b></td>
					<td>%s</td>
					<td><span class="doctor-tag">%s</span></td>
					<td style="text-align:right;">
						<a href="/delete?id=%d" class="btn btn-delete">🗑️</a>
					</td>
				</tr>`, pName, pDate, pDoc, id)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
			<div class="container">
				<div class="user-nav">
					<span style="font-weight:800; font-size:20px;">🌿 HealthTech</span>
					<div>
						<span class="email-badge">%s</span>
						<a href="/logout" style="font-size:12px; color:gray; margin-left:10px;">Выйти</a>
					</div>
				</div>
				
				<div class="card">
					<h1>➕ Новая запись</h1>
					<form action="/save" method="POST">
						<div class="form-group">
							<label>ФИО Пациента</label>
							<input type="text" name="patient_name" placeholder="Введите имя" required>
						</div>
						<div class="form-grid">
							<div class="form-group">
								<label>Дата</label>
								<input type="date" name="date" required>
							</div>
							<div class="form-group">
								<label>Врач</label>
								<select name="doctor">
									<option>Терапевт</option><option>Кардиолог</option>
									<option>Стоматолог</option><option>Невролог</option>
								</select>
							</div>
						</div>
						<button type="submit" class="btn btn-primary">Записать</button>
					</form>
				</div>

				<div class="card">
					<h1>📅 Журнал записей</h1>
					<table>
						<thead><tr><th>Пациент</th><th>Дата</th><th>Врач</th><th></th></tr></thead>
						<tbody>%s</tbody>
					</table>
				</div>
			</div>
		</body></html>`, sharedStyles, email, rowsHtml)
	})

	// Сохранение
	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		email := getEmailFromCookie(r)
		if r.Method == http.MethodPost && email != "" {
			db.Exec("INSERT INTO appointments (patient_name, appointment_date, doctor_name, user_email) VALUES ($1, $2, $3, $4)",
				r.FormValue("patient_name"), r.FormValue("date"), r.FormValue("doctor"), email)
		}
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

	// Удаление
	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		email := getEmailFromCookie(r)
		id := r.URL.Query().Get("id")
		if email != "" && id != "" {
			db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, email)
		}
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

	// Выход
	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:   "user_session",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
