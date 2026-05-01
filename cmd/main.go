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
		log.Fatal(err)
	}

	// Инициализация базы (чтобы не было ошибок при загрузке страниц)
	if db != nil {
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			diagnosis TEXT DEFAULT 'Диагноз еще не поставлен'
		)`)
	}

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/verify-otp", handleVerifyOTP) // Теперь пропускает всех
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/logout", handleLogout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Сервер запущен на порту %s. Вход свободный!", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")

	style := `
	<style>
		body { font-family: 'Segoe UI', sans-serif; background: #f4f7f9; text-align: center; padding-top: 50px; }
		.card { background: white; display: inline-block; padding: 40px; border-radius: 15px; box-shadow: 0 4px 15px rgba(0,0,0,0.1); width: 350px; }
		input { width: 100%; padding: 12px; margin: 15px 0; border: 1px solid #ddd; border-radius: 8px; text-align: center; box-sizing: border-box; }
		button { width: 100%; padding: 12px; background: #007bff; color: white; border: none; border-radius: 8px; cursor: pointer; font-weight: bold; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "%s<div class='card'><h1>HealthOS</h1><p style='color:red;'>Временный вход (без бота)</p><form action='/verify-otp' method='POST'><input name='otp' placeholder='Введите любые цифры'><button type='submit'>Войти как Доктор</button></form></div>", style)
		return
	}

	name, _ := r.Cookie("user_name")

	fmt.Fprintf(w, "%s<div class='card'><h1>🏥 Кабинет Доктора</h1><p>Добро пожаловать, %s</p><hr>", style, name.Value)

	// Показываем тестовый список пациентов (даже если база пуста)
	fmt.Fprint(w, "<div style='text-align:left;'><b>Пациенты:</b><br><br>1. Тестовый Пациент <button>Изменить</button></div>")

	fmt.Fprint(w, "<br><br><a href='/logout' style='color:gray;'>Выйти</a></div>")
}

// ЭТА ФУНКЦИЯ ТЕПЕРЬ ПРОСТО ПУСКАЕТ ВНУТРЬ
func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	// Имитируем успешный вход
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: fmt.Sprint(MY_TG_ID), Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: "doctor", Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_name", Value: "Admin (Test)", Path: "/"})

	http.Redirect(w, r, "/", 302)
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "user_name", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}
