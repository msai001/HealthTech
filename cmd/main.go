package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
)

var db *sql.DB

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("Ошибка подключения к БД: %v", err)
	}

	// ФИКС БАЗЫ: Принудительно добавляем все колонки
	if db != nil {
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			diagnosis TEXT DEFAULT 'Диагноз не установлен',
			priority TEXT DEFAULT 'Средний'
		)`)
		// Добавляем роли и приоритеты на случай, если таблица уже была создана ранее без них
		_, _ = db.Exec(`ALTER TABLE appointments ADD COLUMN IF NOT EXISTS priority TEXT DEFAULT 'Средний'`)
	}

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/login-doctor", handleLoginDoctor)
	http.HandleFunc("/login-patient", handleLoginPatient)
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/logout", handleLogout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Сервер запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")
	role, _ := r.Cookie("user_role")

	// Тот самый "охуенный" дизайн
	style := `
	<style>
		:root { --primary: #2563eb; --bg: #f1f5f9; --card: #ffffff; }
		body { font-family: 'Segoe UI', sans-serif; background: var(--bg); margin: 0; display: flex; justify-content: center; min-height: 100vh; }
		.container { width: 100%; max-width: 600px; padding: 40px 20px; }
		.glass-card { background: var(--card); border-radius: 20px; padding: 30px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); }
		.btn { display: block; width: 100%; padding: 15px; margin: 10px 0; border-radius: 12px; border: 1px solid #e2e8f0; text-decoration: none; color: #1e293b; font-weight: bold; text-align: center; transition: 0.3s; }
		.btn-main { background: var(--primary); color: white; border: none; }
		.btn:hover { transform: translateY(-2px); box-shadow: 0 5px 15px rgba(37,99,235,0.2); }
		.patient-card { background: #f8fafc; padding: 15px; border-radius: 12px; margin-bottom: 10px; border-left: 4px solid var(--primary); }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, `%s<div class="container"><div class="glass-card" style="text-align:center;">
			<h1 style="color:var(--primary)">HealthOS</h1>
			<p>Выберите роль для входа:</p>
			<a href="/login-doctor" class="btn">👨‍⚕️ Войти как Врач</a>
			<a href="/login-patient" class="btn">👤 Войти как Пациент</a>
		</div></div>`, style)
		return
	}

	fmt.Fprintf(w, "%s<div class='container'>", style)
	if role.Value == "doctor" {
		fmt.Fprintf(w, `<div class="glass-card"><h2>Кабинет врача</h2><hr>`)
		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments")
		for rows.Next() {
			var tid int64
			var name, diag string
			rows.Scan(&tid, &name, &diag)
			fmt.Fprintf(w, `<div class="patient-card">
				<b>%s</b><br>
				<form action="/save-diagnosis" method="POST" style="margin-top:10px; display:flex; gap:5px;">
					<input type="hidden" name="tg_id" value="%d">
					<input type="text" name="diag" value="%s" style="flex-grow:1; padding:5px;">
					<button class="btn-main" style="padding:5px 10px; border-radius:5px; cursor:pointer;">OK</button>
				</form>
			</div>`, name, tid, diag)
		}
		rows.Close()
	} else {
		fmt.Fprintf(w, `<div class="glass-card" style="text-align:center;">
			<h1>Моя Карта</h1>
			<p>Ваш статус обновлен врачом</p>
			<div style="background:#f0f7ff; padding:20px; border-radius:15px; margin:20px 0;">
				<b>Диагноз:</b><br>Загрузка... (Нажмите Обновить)
			</div>
		</div>`)
	}
	fmt.Fprintf(w, `<br><a href="/logout" style="color:gray;">Выйти</a></div>`)
}

func handleLoginDoctor(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "1", Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: "doctor", Path: "/"})
	http.Redirect(w, r, "/", 302)
}

func handleLoginPatient(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "999", Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: "patient", Path: "/"})
	http.Redirect(w, r, "/", 302)
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	db.Exec("UPDATE appointments SET diagnosis = $1 WHERE tg_id = $2", r.FormValue("diag"), r.FormValue("tg_id"))
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}
