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
		log.Fatal(err)
	}

	if db != nil {
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			diagnosis TEXT DEFAULT 'Диагноз не установлен',
			priority TEXT DEFAULT 'Средний'
		)`)

		// Создаем тестового пациента, если база пуста
		_, _ = db.Exec(`INSERT INTO appointments (tg_id, patient_name, diagnosis, priority) 
			VALUES (999, 'Александр Петров (Пациент)', 'ОРВИ, легкая форма', 'Низкий') 
			ON CONFLICT DO NOTHING`)
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
	log.Printf("HealthOS запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")
	role, _ := r.Cookie("user_role")

	style := `
	<style>
		:root { --primary: #2563eb; --bg: #f8fafc; --text: #1e293b; --card: #ffffff; }
		body { font-family: 'Inter', system-ui, sans-serif; background: var(--bg); color: var(--text); margin: 0; display: flex; justify-content: center; min-height: 100vh; }
		.container { width: 100%; max-width: 800px; padding: 40px 20px; }
		.glass-card { background: var(--card); border-radius: 24px; padding: 32px; box-shadow: 0 10px 40px -10px rgba(0,0,0,0.05); border: 1px solid rgba(255,255,255,0.1); }
		h1 { font-size: 2.5rem; font-weight: 800; letter-spacing: -1px; margin-bottom: 8px; color: var(--primary); }
		.role-btn { display: block; width: 100%; padding: 20px; margin-bottom: 16px; border-radius: 16px; border: 2px solid #e2e8f0; background: white; cursor: pointer; text-align: left; transition: 0.2s; text-decoration: none; color: inherit; }
		.role-btn:hover { border-color: var(--primary); background: #eff6ff; transform: translateY(-2px); }
		.patient-row { background: #f1f5f9; border-radius: 12px; padding: 16px; margin-bottom: 12px; }
		.btn-save { background: var(--primary); color: white; border: none; padding: 10px 20px; border-radius: 8px; font-weight: bold; cursor: pointer; }
		.badge { padding: 4px 12px; border-radius: 20px; font-size: 12px; font-weight: 600; }
		.p-crit { background: #fee2e2; color: #dc2626; }
		input, select { padding: 10px; border: 1px solid #cbd5e1; border-radius: 8px; }
	</style>`

	// ЭКРАН ЛОГИНА
	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, `%s<div class="container"><div class="glass-card" style="text-align:center;">
			<h1>HealthOS</h1><p style="color:#64748b; margin-bottom:40px;">Выберите способ входа в систему</p>
			<a href="/login-doctor" class="role-btn"><b>👨‍⚕️ Войти как Врач</b><br><small>Управление пациентами и диагнозами</small></a>
			<a href="/login-patient" class="role-btn"><b>👤 Войти как Пациент</b><br><small>Просмотр своей медицинской карты</small></a>
		</div></div>`, style)
		return
	}

	fmt.Fprintf(w, "%s<div class='container'>", style)

	// ИНТЕРФЕЙС ВРАЧА
	if role.Value == "doctor" {
		fmt.Fprintf(w, `<div class="glass-card">
			<div style="display:flex; justify-content:space-between; align-items:center;">
				<h1>Кабинет врача</h1>
				<a href="/logout" style="color:#64748b; text-decoration:none;">Выход</a>
			</div>
			<hr style="border:0; border-top:1px solid #e2e8f0; margin: 24px 0;">`)

		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis, priority FROM appointments ORDER BY priority DESC")
		for rows.Next() {
			var tid int64
			var name, diag, prio string
			rows.Scan(&tid, &name, &diag, &prio)
			fmt.Fprintf(w, `
				<div class="patient-row">
					<div style="display:flex; justify-content:space-between; margin-bottom:10px;">
						<b>%s</b> <span class="badge p-crit">%s</span>
					</div>
					<form action="/save-diagnosis" method="POST" style="display:flex; gap:8px;">
						<input type="hidden" name="tg_id" value="%d">
						<input type="text" name="diag" value="%s" style="flex-grow:1;">
						<select name="priority">
							<option value="Низкий" %s>Низкий</option>
							<option value="Средний" %s>Средний</option>
							<option value="Критический" %s>Критический</option>
						</select>
						<button class="btn-save">OK</button>
					</form>
				</div>`, name, prio, tid, diag, sel(prio, "Низкий"), sel(prio, "Средний"), sel(prio, "Критический"))
		}
		fmt.Fprintf(w, "</div>")

		// ИНТЕРФЕЙС ПАЦИЕНТА
	} else {
		var name, diag string
		_ = db.QueryRow("SELECT patient_name, diagnosis FROM appointments WHERE tg_id = 999").Scan(&name, &diag)
		fmt.Fprintf(w, `<div class="glass-card" style="text-align:center;">
			<div style="background:#eff6ff; width:80px; height:80px; border-radius:50%%; margin:0 auto 20px; display:flex; align-items:center; justify-content:center; font-size:32px;">👤</div>
			<h1>Личная карта</h1>
			<p>Пациент: <b>%s</b></p>
			<div style="background:#f8fafc; padding:32px; border-radius:20px; border:2px dashed #e2e8f0; margin-top:24px;">
				<small style="text-transform:uppercase; color:#64748b; font-weight:bold;">Текущий диагноз</small>
				<h2 style="margin:12px 0 0;">%s</h2>
			</div>
			<br><a href="/logout" style="color:#64748b;">Выйти из профиля</a>
		</div>`, name, diag)
	}
	fmt.Fprintf(w, "</div>")
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
	db.Exec("UPDATE appointments SET diagnosis = $1, priority = $2 WHERE tg_id = $3", r.FormValue("diag"), r.FormValue("priority"), r.FormValue("tg_id"))
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}

func sel(c, t string) string {
	if c == t {
		return "selected"
	}
	return ""
}
