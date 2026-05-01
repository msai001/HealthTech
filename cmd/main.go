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

	// Принудительная чистка и создание таблицы при запуске
	if db != nil {
		_, _ = db.Exec("DROP TABLE IF EXISTS appointments CASCADE") // Удаляем старое, если мешает
		_, _ = db.Exec(`CREATE TABLE appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			diagnosis TEXT DEFAULT 'Ожидает осмотра',
			priority TEXT DEFAULT 'Средний'
		)`)

		// Добавляем тестового пациента, чтобы ты сразу видел результат
		_, _ = db.Exec(`INSERT INTO appointments (tg_id, patient_name, diagnosis, priority) 
			VALUES (999, 'Тестовый Пациент', 'Первичный осмотр', 'Низкий')`)
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
		body { font-family: 'Inter', sans-serif; background: #f4f7fa; display: flex; justify-content: center; padding: 50px; }
		.card { background: white; padding: 30px; border-radius: 20px; box-shadow: 0 10px 30px rgba(0,0,0,0.1); width: 100%; max-width: 500px; text-align: center; }
		.btn { display: block; width: 100%; padding: 15px; margin: 10px 0; border-radius: 12px; border: none; background: #2563eb; color: white; font-weight: bold; cursor: pointer; text-decoration: none; }
		.patient-box { text-align: left; background: #f8fafc; padding: 15px; border-radius: 12px; margin-bottom: 10px; border-left: 5px solid #2563eb; }
		input { width: 100%; padding: 8px; margin-top: 5px; border: 1px solid #ddd; border-radius: 5px; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "%s<div class='card'><h1>HealthOS</h1><a href='/login-doctor' class='btn'>Войти как Врач</a><a href='/login-patient' class='btn' style='background:#10b981'>Войти как Пациент</a></div>", style)
		return
	}

	fmt.Fprintf(w, "%s<div class='card'>", style)
	if role.Value == "doctor" {
		fmt.Fprintf(w, "<h2>Панель Врача</h2>")
		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments")
		for rows.Next() {
			var tid int64
			var name, diag string
			rows.Scan(&tid, &name, &diag)
			fmt.Fprintf(w, `<div class='patient-box'><b>%s</b><form action='/save-diagnosis' method='POST'><input type='hidden' name='tg_id' value='%d'><input name='diag' value='%s'><button type='submit' style='margin-top:5px; cursor:pointer;'>Сохранить</button></form></div>`, name, tid, diag)
		}
	} else {
		var diag string
		_ = db.QueryRow("SELECT diagnosis FROM appointments WHERE tg_id = 999").Scan(&diag)
		fmt.Fprintf(w, "<h2>Моя Карта</h2><div class='patient-box' style='text-align:center;'><b>Ваш диагноз:</b><br><br><span style='font-size:20px; color:#2563eb;'>%s</span></div>", diag)
	}
	fmt.Fprintf(w, "<br><a href='/logout' style='color:gray;'>Выход</a></div>")
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
