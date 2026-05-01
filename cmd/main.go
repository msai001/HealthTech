package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
)

const MY_TG_ID = 58392011

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
			diagnosis TEXT DEFAULT 'Диагноз не установлен'
		)`)

		// Добавим пару разных пациентов для теста
		_, _ = db.Exec(`INSERT INTO appointments (tg_id, patient_name, diagnosis) 
			VALUES (123, 'Иван Иванов', 'Диагноз не установлен'), (456, 'Алия Серикова', 'Грипп, покой') 
			ON CONFLICT DO NOTHING`)
	}

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/logout", handleLogout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")

	// Расширенный CSS со стилями для поиска и статусов
	style := `
	<style>
		body { font-family: 'Inter', sans-serif; background: #f8f9fa; margin: 0; padding: 20px; color: #333; }
		.container { max-width: 900px; margin: 0 auto; }
		.card { background: white; padding: 25px; border-radius: 16px; box-shadow: 0 10px 30px rgba(0,0,0,0.05); margin-bottom: 20px; }
		.header { display: flex; justify-content: space-between; align-items: center; border-bottom: 2px solid #1a73e8; padding-bottom: 15px; }
		.search-box { width: 100%; padding: 12px; margin: 20px 0; border: 1px solid #ddd; border-radius: 10px; font-size: 16px; }
		.patient-item { background: #fff; border: 1px solid #eee; padding: 20px; border-radius: 12px; margin-bottom: 10px; transition: 0.3s; }
		.patient-item:hover { transform: translateY(-2px); box-shadow: 0 5px 15px rgba(0,0,0,0.1); }
		.status-badge { display: inline-block; padding: 4px 12px; border-radius: 20px; font-size: 12px; font-weight: bold; }
		.status-none { background: #ffe8e8; color: #d9534f; }
		.status-ok { background: #e8f5e9; color: #2e7d32; }
		.btn { background: #1a73e8; color: white; border: none; padding: 10px 20px; border-radius: 8px; cursor: pointer; }
		.stats-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 15px; margin-bottom: 20px; }
		.stat-card { background: #fff; padding: 15px; border-radius: 12px; text-align: center; border: 1px solid #eee; }
	</style>
	<script>
		function filterPatients() {
			let input = document.getElementById('search').value.toLowerCase();
			let items = document.getElementsByClassName('patient-item');
			for (let i = 0; i < items.length; i++) {
				let name = items[i].getElementsByClassName('p-name')[0].innerText.toLowerCase();
				items[i].style.display = name.includes(input) ? "" : "none";
			}
		}
	</script>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "%s<div class='container'><div class='card'><h1>HealthOS</h1><form action='/verify-otp' method='POST'><button class='btn' style='width:100%%'>Войти как Главврач</button></form></div></div>", style)
		return
	}

	// Считаем статистику
	var total, diagnosed int
	_ = db.QueryRow("SELECT COUNT(*) FROM appointments").Scan(&total)
	_ = db.QueryRow("SELECT COUNT(*) FROM appointments WHERE diagnosis != 'Диагноз не установлен'").Scan(&diagnosed)

	fmt.Fprintf(w, "%s<div class='container'>", style)
	fmt.Fprintf(w, `
		<div class="card header">
			<h1>🏥 HealthOS Dashboard</h1>
			<a href="/logout" style="color: #666; text-decoration: none;">Выйти</a>
		</div>

		<div class="stats-grid">
			<div class="stat-card"><b>Всего</b><br><span style="font-size:24px">%d</span></div>
			<div class="stat-card"><b>С диагнозом</b><br><span style="font-size:24px; color: green">%d</span></div>
			<div class="stat-card"><b>Ожидают</b><br><span style="font-size:24px; color: orange">%d</span></div>
		</div>

		<input type="text" id="search" class="search-box" onkeyup="filterPatients()" placeholder="Поиск по фамилии пациента...">
	`, total, diagnosed, total-diagnosed)

	if db != nil {
		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments ORDER BY id DESC")
		for rows.Next() {
			var tid int64
			var name, diag string
			rows.Scan(&tid, &name, &diag)

			badgeClass := "status-ok"
			statusText := "Обработан"
			if diag == "Диагноз не установлен" {
				badgeClass = "status-none"
				statusText = "Новый"
			}

			fmt.Fprintf(w, `
				<div class="patient-item">
					<div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px;">
						<span class="p-name" style="font-size: 18px; font-weight: bold;">%s</span>
						<span class="status-badge %s">%s</span>
					</div>
					<form action="/save-diagnosis" method="POST" style="display: flex; gap: 10px;">
						<input type="hidden" name="tg_id" value="%d">
						<input type="text" name="diag" value="%s" style="flex-grow: 1; padding: 10px; border-radius: 8px; border: 1px solid #ddd;">
						<button class="btn" type="submit">Сохранить</button>
					</form>
				</div>`, name, badgeClass, statusText, tid, diag)
		}
		rows.Close()
	}
	fmt.Fprintf(w, "</div>")
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		db.Exec("UPDATE appointments SET diagnosis = $1 WHERE tg_id = $2", r.FormValue("diag"), r.FormValue("tg_id"))
	}
	http.Redirect(w, r, "/", 302)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "admin", Path: "/"})
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}
