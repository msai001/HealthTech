package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

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
		body { font-family: 'Inter', system-ui, sans-serif; background: var(--bg); color: var(--text); line-height: 1.5; }
		.container { max-width: 900px; margin: 40px auto; padding: 0 20px; }
		.card { background: white; padding: 30px; border-radius: 20px; box-shadow: 0 10px 25px rgba(0,0,0,0.03); margin-bottom: 25px; border: 1px solid #e2e8f0; }
		h1 { font-size: 24px; font-weight: 800; color: #0f172a; margin-bottom: 20px; display: flex; align-items: center; gap: 10px; }
		.user-nav { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
		.email-badge { background: #dcfce7; color: #166534; padding: 5px 15px; border-radius: 50px; font-size: 13px; font-weight: 600; }
		.btn { cursor: pointer; border: none; border-radius: 10px; font-weight: 700; transition: all 0.2s; text-decoration: none; display: inline-flex; align-items: center; justify-content: center; }
		.btn-primary { background: var(--primary); color: white; width: 100%; padding: 14px; font-size: 16px; }
		.btn-primary:hover { background: var(--primary-hover); transform: translateY(-1px); }
		.btn-delete { background: #fee2e2; color: var(--danger); padding: 8px; font-size: 12px; }
		.btn-delete:hover { background: var(--danger); color: white; }
		.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; margin-bottom: 20px; }
		.form-group { text-align: left; margin-bottom: 15px; }
		label { display: block; margin-bottom: 6px; font-size: 13px; font-weight: 600; color: #64748b; }
		input, select { width: 100%; padding: 12px; border: 1px solid #e2e8f0; border-radius: 10px; font-size: 15px; outline: none; }
		input:focus { border-color: var(--primary); box-shadow: 0 0 0 3px rgba(16, 185, 129, 0.1); }
		table { width: 100%; border-collapse: collapse; margin-top: 10px; }
		th { text-align: left; padding: 12px; font-size: 12px; color: #94a3b8; text-transform: uppercase; border-bottom: 2px solid #f1f5f9; }
		td { padding: 16px 12px; border-bottom: 1px solid #f1f5f9; font-size: 14px; }
		.doctor-tag { background: #f1f5f9; color: #475569; padding: 3px 8px; border-radius: 6px; font-size: 12px; font-weight: 600; }
		@media (max-width: 600px) { .form-grid { grid-template-columns: 1fr; } }
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

func main() {
	initDB()

	// Главная
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
			<div class="container" style="text-align:center; margin-top: 100px;">
				<div class="card">
					<div style="font-size:48px;">🌿</div>
					<h1>HealthTech Pro</h1>
					<p style="color:#64748b; margin-bottom:30px;">Профессиональное управление медицинскими записями</p>
					<a href="%s" class="btn btn-primary">Войти через Google</a>
				</div>
			</div>
		</body></html>`, sharedStyles, url)
	})

	// Панель управления
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

		// Загрузка записей
		rows, _ := db.Query("SELECT id, patient_name, appointment_date, doctor_name FROM appointments WHERE user_email = $1 ORDER BY id DESC", userInfo.Email)
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
						<a href="/delete?id=%d&email=%s" class="btn btn-delete" onclick="return confirm('Удалить эту запись?')">🗑️</a>
					</td>
				</tr>`, pName, pDate, pDoc, id, userInfo.Email)
		}

		if rowsHtml == "" {
			rowsHtml = `<tr><td colspan="4" style="text-align:center; padding:40px; color:#94a3b8;">У вас пока нет активных записей</td></tr>`
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
			<div class="container">
				<div class="user-nav">
					<span style="font-weight:800; font-size:20px;">🌿 HealthTech</span>
					<span class="email-badge">%s</span>
				</div>
				
				<div class="card">
					<h1>➕ Новая запись</h1>
					<form action="/save" method="POST">
						<input type="hidden" name="user_email" value="%s">
						<div class="form-group">
							<label>ФИО Пациента</label>
							<input type="text" name="patient_name" placeholder="Введите полное имя" required>
						</div>
						<div class="form-grid">
							<div class="form-group">
								<label>Дата приема</label>
								<input type="date" name="date" required>
							</div>
							<div class="form-group">
								<label>Врач</label>
								<select name="doctor">
									<option>Терапевт</option><option>Невролог</option>
									<option>Кардиолог</option><option>Хирург</option>
								</select>
							</div>
						</div>
						<button type="submit" class="btn btn-primary">Создать запись</button>
					</form>
				</div>

				<div class="card">
					<h1>📅 Журнал приемов</h1>
					<table>
						<thead><tr><th>Пациент</th><th>Дата</th><th>Врач</th><th></th></tr></thead>
						<tbody>%s</tbody>
					</table>
				</div>
			</div>
		</body></html>`, sharedStyles, userInfo.Email, userInfo.Email, rowsHtml)
	})

	// Сохранение
	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			db.Exec("INSERT INTO appointments (patient_name, appointment_date, doctor_name, user_email) VALUES ($1, $2, $3, $4)",
				r.FormValue("patient_name"), r.FormValue("date"), r.FormValue("doctor"), r.FormValue("user_email"))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<script>window.history.back(); setTimeout(()=>location.reload(), 100);</script>`)
		}
	})

	// Удаление
	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		email := r.URL.Query().Get("email")
		// Простая проверка: удаляем только если email совпадает
		db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, email)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<script>window.history.back(); setTimeout(()=>location.reload(), 100);</script>`)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
