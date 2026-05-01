package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// !!! ЗАМЕНИ НА СВОЙ ID И ССЫЛКУ !!!
const MY_TG_ID = 58392011
const BOT_LINK = "https://t.me/health_os_bot"

var db *sql.DB

func main() {
	rand.Seed(time.Now().UnixNano())

	dbURL := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("DB Error: %v", err)
	}

	// Создаем таблицу, если её нет
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			totp_secret TEXT,
			diagnosis TEXT DEFAULT 'Диагноз еще не поставлен'
		)`)

	// ЗАПУСК БОТА (Полный цикл)
	go startTelegramBot()

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/logout", handleLogout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server live on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		return
	}
	apiURL := "https://api.telegram.org/bot" + token + "/"
	offset := 0

	log.Println("Бот начал опрос Telegram API...")

	for {
		// Long Polling
		resp, err := http.Get(fmt.Sprintf("%sgetUpdates?offset=%d&timeout=20", apiURL, offset))
		if err != nil || resp == nil {
			time.Sleep(5 * time.Second)
			continue
		}

		var updates struct {
			Ok     bool `json:"ok"`
			Result []struct {
				UpdateID int `json:"update_id"`
				Message  struct {
					Chat struct{ ID int64 }         `json:"chat"`
					Text string                     `json:"text"`
					From struct{ FirstName string } `json:"from"`
				} `json:"message"`
			} `json:"result"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&updates); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		for _, u := range updates.Result {
			if strings.HasPrefix(u.Message.Text, "/start") {
				code := fmt.Sprintf("%06d", rand.Intn(1000000))
				tgID := u.Message.Chat.ID
				name := u.Message.From.FirstName

				// Записываем в базу
				_, dbErr := db.Exec(`
					INSERT INTO appointments (tg_id, patient_name, totp_secret) 
					VALUES ($1, $2, $3) 
					ON CONFLICT (tg_id) DO UPDATE SET totp_secret = $3`,
					tgID, name, code)

				if dbErr != nil {
					log.Printf("Ошибка записи кода: %v", dbErr)
				} else {
					log.Printf("Код %s сгенерирован для %s", code, name)
					// Используем sendMessage вместо getUpdates для отправки
					http.Get(fmt.Sprintf("%ssendMessage?chat_id=%d&text=Ваш код HealthOS: %s", apiURL, tgID, code))
				}
			}
			offset = u.UpdateID + 1
		}
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")

	// СТИЛЬ
	fmt.Fprint(w, "<style>body{font-family:sans-serif; background:#f4f7f9; text-align:center; padding-top:50px;} .card{background:white; display:inline-block; padding:30px; border-radius:10px; box-shadow:0 2px 10px rgba(0,0,0,0.1);}</style>")

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "<div class='card'><h1>HealthOS</h1><p>Нажмите /start в боте</p><a href='%s' target='_blank'>Перейти к боту</a><br><br><form action='/verify-otp' method='POST'><input name='otp' placeholder='Код' style='padding:10px; text-align:center;' required><br><br><button style='padding:10px 20px; background:#28a745; color:white; border:none; border-radius:5px;'>Войти</button></form></div>", BOT_LINK)
		return
	}

	role, _ := r.Cookie("user_role")
	name, _ := r.Cookie("user_name")

	fmt.Fprintf(w, "<div class='card'>")
	if role.Value == "doctor" {
		fmt.Fprintf(w, "<h1>👨‍⚕️ Кабинет Доктора</h1><p>Доктор: %s</p><hr>", name.Value)
		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments WHERE tg_id != $1", MY_TG_ID)
		for rows.Next() {
			var pID int64
			var pName, pDiag string
			rows.Scan(&pID, &pName, &pDiag)
			fmt.Fprintf(w, "<div style='margin-bottom:10px;'>%s: <form action='/save-diagnosis' method='POST' style='display:inline;'><input type='hidden' name='tg_id' value='%d'><input name='diagnosis' value='%s'><button>OK</button></form></div>", pName, pID, pDiag)
		}
		rows.Close()
	} else {
		var d string
		db.QueryRow("SELECT diagnosis FROM appointments WHERE tg_id = $1", cID.Value).Scan(&d)
		fmt.Fprintf(w, "<h1>🏥 Кабинет Пациента</h1><p>Добро пожаловать, %s</p><div style='background:#e7f3ff; padding:15px;'><b>Ваш диагноз:</b> %s</div>", name.Value, d)
	}
	fmt.Fprint(w, "<br><br><a href='/logout'>Выйти</a></div>")
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	input := r.FormValue("otp")
	var tgID int64
	var name string
	err := db.QueryRow("SELECT tg_id, patient_name FROM appointments WHERE totp_secret = $1", input).Scan(&tgID, &name)

	if err != nil {
		fmt.Fprint(w, "Неверный код! <a href='/'>Назад</a>")
		return
	}

	role := "patient"
	if tgID == MY_TG_ID {
		role = "doctor"
	}

	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: fmt.Sprint(tgID), Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: role, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_name", Value: name, Path: "/"})
	// Стираем код
	db.Exec("UPDATE appointments SET totp_secret = '' WHERE tg_id = $1", tgID)
	http.Redirect(w, r, "/", 302)
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	db.Exec("UPDATE appointments SET diagnosis = $1 WHERE tg_id = $2", r.FormValue("diagnosis"), r.FormValue("tg_id"))
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "user_name", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}
