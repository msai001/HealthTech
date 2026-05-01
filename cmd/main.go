package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
)

// Твой ID для роли доктора
const MY_TG_ID = 58392011

var db *sql.DB

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("Ошибка БД: %v", err)
	}

	// Инициализация таблицы с колонкой диагноза
	if db != nil {
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			diagnosis TEXT DEFAULT 'Диагноз не установлен'
		)`)

		// Добавим тестового пациента для демонстрации, если таблица пуста
		_, _ = db.Exec(`INSERT INTO appointments (tg_id, patient_name, diagnosis) 
			VALUES (123, 'Иван Иванов (Тест)', 'Требуется осмотр') 
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
	log.Printf("HealthOS запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")

	style := `
	<style>
		body { font-family: 'Segoe UI', sans-serif; background: #f0f2f5; margin: 0; padding: 20px; }
		.container { max-width: 800px; margin: 0 auto; }
		.card { background: white; padding: 25px; border-radius: 12px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); margin-bottom: 20px; }
		h1 { color: #1a73e8; margin-top: 0; }
		.patient-item { border-bottom: 1px solid #eee; padding: 15px 0; display: flex; justify-content: space-between; align-items: center; }
		input[type="text"] { padding: 8px; border: 1px solid #ddd; border-radius: 6px; width: 60%; }
		.btn { background: #1a73e8; color: white; border: none; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-weight: bold; }
		.btn:hover { background: #1557b0; }
		.logout { color: #666; text-decoration: none; font-size: 14px; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "%s<div class='container'><div class='card'><h1>HealthOS Login</h1><form action='/verify-otp' method='POST'><input name='otp' placeholder='Введите любой код' style='width:100%%; padding:10px; margin-bottom:10px;'><br><button class='btn' style='width:100%%'>Войти в систему</button></form></div></div>", style)
		return
	}

	fmt.Fprintf(w, "%s<div class='container'>", style)
	fmt.Fprintf(w, "<div class='card'><h1>👨‍⚕️ Панель управления HealthOS</h1><p>Текущий врач: <b>Admin</b></p><a href='/logout' class='logout'>Выйти из системы</a></div>")

	fmt.Fprintf(w, "<div class='card'><h3>Список пациентов и назначений</h3>")

	// Тянем данные из базы
	if db != nil {
		rows, err := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments")
		if err == nil {
			for rows.Next() {
				var tid int64
				var name, diag string
				rows.Scan(&tid, &name, &diag)
				fmt.Fprintf(w, `
					<div class="patient-item">
						<div style="width: 30%%"><b>%s</b><br><small>ID: %d</small></div>
						<form action="/save-diagnosis" method="POST" style="width: 70%%; display: flex; gap: 10px;">
							<input type="hidden" name="tg_id" value="%d">
							<input type="text" name="diag" value="%s">
							<button class="btn" type="submit">OK</button>
						</form>
					</div>`, name, tid, tid, diag)
			}
			rows.Close()
		}
	}
	fmt.Fprintf(w, "</div></div>")
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		tid := r.FormValue("tg_id")
		diag := r.FormValue("diag")
		if db != nil {
			_, err := db.Exec("UPDATE appointments SET diagnosis = $1 WHERE tg_id = $2", diag, tid)
			if err != nil {
				log.Printf("Ошибка сохранения: %v", err)
			}
		}
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
