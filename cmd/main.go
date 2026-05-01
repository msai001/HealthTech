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
		_, _ = db.Exec(`ALTER TABLE appointments ADD COLUMN IF NOT EXISTS priority TEXT DEFAULT 'Средний'`)
	}

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/clear-db", handleClearDB) // Секретная функция
	http.HandleFunc("/verify-otp", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "admin", Path: "/"})
		http.Redirect(w, r, "/", 302)
	})
	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/", 302)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")

	style := `
	<style>
		:root { --primary: #1a73e8; --danger: #d93025; --success: #188038; --warning: #f9ab00; }
		body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background: #f1f3f4; margin: 0; padding: 20px; color: #202124; }
		.container { max-width: 900px; margin: 0 auto; }
		.card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 1px 3px rgba(0,0,0,0.12); margin-bottom: 16px; position: relative; }
		.header-panel { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
		.stats-container { display: flex; gap: 16px; margin-bottom: 24px; }
		.stat-box { flex: 1; background: white; padding: 16px; border-radius: 8px; border-bottom: 4px solid var(--primary); text-align: center; }
		.stat-box h2 { margin: 0; font-size: 28px; }
		.stat-box p { margin: 4px 0 0; color: #5f6368; font-size: 14px; }
		.progress-bg { background: #e0e0e0; height: 12px; border-radius: 6px; overflow: hidden; margin: 10px 0; }
		.progress-fill { background: var(--success); height: 100%; transition: width 0.5s ease; }
		.patient-card { border-left: 6px solid #dadce0; transition: transform 0.2s; }
		.patient-card:hover { transform: scale(1.01); }
		.crit { border-left-color: var(--danger); }
		.warn { border-left-color: var(--warning); }
		.btn { padding: 10px 20px; border-radius: 4px; border: none; cursor: pointer; font-weight: 500; transition: 0.2s; }
		.btn-primary { background: var(--primary); color: white; }
		.btn-danger { background: transparent; color: var(--danger); border: 1px solid var(--danger); font-size: 12px; }
		.btn-danger:hover { background: var(--danger); color: white; }
		select, input { padding: 8px; border: 1px solid #dadce0; border-radius: 4px; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "%s<div class='container' style='text-align:center; padding-top:100px;'><h1>HealthOS Login</h1><button onclick=\"location.href='/verify-otp'\" class='btn btn-primary'>Войти в панель управления</button></div>", style)
		return
	}

	// Собираем статистику
	var total, diagnosed int
	_ = db.QueryRow("SELECT COUNT(*) FROM appointments").Scan(&total)
	_ = db.QueryRow("SELECT COUNT(*) FROM appointments WHERE diagnosis != 'Диагноз не установлен'").Scan(&diagnosed)

	percent := 0
	if total > 0 {
		percent = (diagnosed * 100) / total
	}

	fmt.Fprintf(w, "%s<div class='container'>", style)
	fmt.Fprintf(w, `
		<div class="header-panel">
			<h1>📊 Аналитика клиники</h1>
			<a href="/logout" style="color: #5f6368; text-decoration: none;">Выйти</a>
		</div>

		<div class="stats-container">
			<div class="stat-box"><h2>%d</h2><p>Пациентов</p></div>
			<div class="stat-box" style="border-color: var(--success)"><h2>%d%%</h2><p>Эффективность</p></div>
			<div class="stat-box" style="border-color: var(--danger)"><button onclick="if(confirm('Удалить всех?')) location.href='/clear-db'" class="btn btn-danger">Очистить базу</button></div>
		</div>

		<div class="card">
			<p>Прогресс обработки записей: <b>%d из %d</b></p>
			<div class="progress-bg"><div class="progress-fill" style="width: %d%%"></div></div>
		</div>
	`, total, percent, diagnosed, total, percent)

	rows, _ := db.Query(`SELECT tg_id, patient_name, diagnosis, priority FROM appointments ORDER BY CASE WHEN priority = 'Критический' THEN 1 WHEN priority = 'Средний' THEN 2 ELSE 3 END ASC`)
	for rows.Next() {
		var tid int64
		var name, diag, prio string
		rows.Scan(&tid, &name, &diag, &prio)

		pClass := ""
		switch prio {
		case "Критический":
			pClass = "crit"
		case "Средний":
			pClass = "warn"
		}

		fmt.Fprintf(w, `
			<div class="card patient-card %s">
				<div style="display:flex; justify-content:space-between; align-items:center;">
					<span style="font-size:18px; font-weight:bold;">%s</span>
					<span style="color:#70757a; font-size:12px;">TG-ID: %d</span>
				</div>
				<form action="/save-diagnosis" method="POST" style="margin-top:12px; display:flex; gap:10px;">
					<input type="hidden" name="tg_id" value="%d">
					<input type="text" name="diag" value="%s" style="flex-grow:2;">
					<select name="priority" style="flex-grow:1;">
						<option value="Низкий" %s>Низкий</option>
						<option value="Средний" %s>Средний</option>
						<option value="Критический" %s>Критический</option>
					</select>
					<button class="btn btn-primary" type="submit">OK</button>
				</form>
			</div>`, pClass, name, tid, tid, diag, sel(prio, "Низкий"), sel(prio, "Средний"), sel(prio, "Критический"))
	}
	fmt.Fprintf(w, "</div>")
}

func handleClearDB(w http.ResponseWriter, r *http.Request) {
	_, _ = db.Exec("DELETE FROM appointments")
	_, _ = db.Exec("DELETE FROM diagnosis_logs")
	http.Redirect(w, r, "/", 302)
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		db.Exec("UPDATE appointments SET diagnosis = $1, priority = $2 WHERE tg_id = $3", r.FormValue("diag"), r.FormValue("priority"), r.FormValue("tg_id"))
	}
	http.Redirect(w, r, "/", 302)
}

func sel(c, t string) string {
	if c == t {
		return "selected"
	}
	return ""
}
