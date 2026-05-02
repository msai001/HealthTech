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
			diagnosis TEXT DEFAULT 'Ожидает осмотра',
			priority TEXT DEFAULT 'Средний'
		)`)
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
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")
	role, _ := r.Cookie("user_role")

	style := `
	<style>
		:root {
			--primary: #007AFF;
			--success: #34C759;
			--bg: #F2F2F7;
			--card-bg: #FFFFFF;
			--text: #1C1C1E;
			--secondary-text: #8E8E93;
		}
		* { box-sizing: border-box; transition: 0.2s ease-in-out; }
		body { 
			font-family: -apple-system, system-ui, sans-serif;
			background-color: var(--bg);
			color: var(--text);
			margin: 0;
			display: flex;
			align-items: center;
			justify-content: center;
			min-height: 100vh;
		}
		.container { width: 100%; max-width: 420px; padding: 15px; }
		.card {
			background: var(--card-bg);
			border-radius: 24px;
			padding: 35px 25px;
			box-shadow: 0 20px 40px rgba(0,0,0,0.06);
			border: 1px solid rgba(0,0,0,0.03);
		}
		.logo-circle {
			width: 60px; height: 60px; background: var(--primary);
			border-radius: 18px; margin: 0 auto 20px;
			display: flex; align-items: center; justify-content: center;
			color: white; font-size: 30px; font-weight: bold;
		}
		h1 { font-size: 26px; font-weight: 800; margin: 0 0 8px; letter-spacing: -0.5px; }
		.subtitle { color: var(--secondary-text); font-size: 15px; margin-bottom: 30px; }
		
		.btn {
			display: flex; align-items: center; justify-content: center;
			width: 100%; padding: 18px; margin: 10px 0;
			border-radius: 16px; border: none;
			font-size: 16px; font-weight: 700;
			cursor: pointer; text-decoration: none;
		}
		.btn-primary { background: var(--primary); color: white; }
		.btn-primary:hover { opacity: 0.9; transform: translateY(-1px); }
		.btn-ghost { background: #F2F2F7; color: var(--text); }
		
		.patient-card {
			text-align: left; background: #FFFFFF;
			border: 1px solid #E5E5EA; border-radius: 18px;
			padding: 16px; margin-bottom: 12px;
		}
		.status-tag {
			font-size: 11px; font-weight: 700; text-transform: uppercase;
			padding: 4px 8px; background: #E5F1FF; color: var(--primary);
			border-radius: 6px; display: inline-block; margin-bottom: 8px;
		}
		input[type="text"] {
			width: 100%; padding: 12px; border-radius: 12px;
			border: 1px solid #D1D1D6; font-size: 15px; outline: none;
		}
		input[type="text"]:focus { border-color: var(--primary); }
		.save-btn {
			background: var(--text); color: white; border: none;
			padding: 10px 20px; border-radius: 10px;
			font-weight: 600; margin-top: 10px; cursor: pointer; width: 100%;
		}
		.logout-link { color: var(--secondary-text); font-size: 13px; text-decoration: none; display: block; margin-top: 25px; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, `%s
		<div class="container">
			<div class="card">
				<div class="logo-circle">H</div>
				<h1>HealthOS</h1>
				<p class="subtitle">Медицинская информационная система</p>
				<a href="/login-doctor" class="btn btn-primary">👨‍⚕️ Кабинет врача</a>
				<a href="/login-patient" class="btn btn-ghost">👤 Личный кабинет пациента</a>
			</div>
		</div>`, style)
		return
	}

	fmt.Fprintf(w, "%s<div class='container'><div class='card'>", style)

	if role.Value == "doctor" {
		fmt.Fprintf(w, "<h1>Приём</h1><p class=\"subtitle\">Управление очередью пациентов</p>")
		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments")
		for rows.Next() {
			var tid int64
			var name, diag string
			rows.Scan(&tid, &name, &diag)
			fmt.Fprintf(w, `
			<div class="patient-card">
				<div class="status-tag">ID: %d</div>
				<div style="font-weight:700; margin-bottom:10px;">%s</div>
				<form action="/save-diagnosis" method="POST">
					<input type="hidden" name="tg_id" value="%d">
					<input type="text" name="diag" value="%s" placeholder="Укажите диагноз...">
					<button type="submit" class="save-btn">Обновить карту</button>
				</form>
			</div>`, tid, name, tid, diag)
		}
		rows.Close()
	} else {
		var diag string
		_ = db.QueryRow("SELECT diagnosis FROM appointments WHERE tg_id = 999").Scan(&diag)
		fmt.Fprintf(w, `
		<h1>Здоровье</h1>
		<p class="subtitle">Ваши медицинские данные</p>
		<div class="patient-card" style="border-left: 5px solid var(--success); background: #F6FFF8;">
			<div style="color: #1E6631; font-size: 13px; font-weight: 600;">АКТУАЛЬНЫЙ ДИАГНОЗ</div>
			<div style="font-size: 24px; font-weight: 800; margin-top: 10px; color: #1E6631;">%s</div>
		</div>`, diag)
	}

	fmt.Fprintf(w, "<a href='/logout' class='logout-link'>Выйти из системы</a></div></div>")
}

// Функции-обработчики без изменений для стабильности
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
