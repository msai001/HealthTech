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

// ЗАМЕНИ НА СВОЙ ID
const MY_TG_ID = 58392011
const BOT_LINK = "https://t.me/health_os_bot"

var db *sql.DB

func main() {
	rand.Seed(time.Now().UnixNano())

	dbURL := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("DB Open Error: %v", err)
	}

	// Инициализируем таблицу "тихо"
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			totp_secret TEXT,
			diagnosis TEXT DEFAULT 'Диагноз еще не поставлен'
		)`)

	go startTelegramBot()

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/logout", handleLogout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	// Не используем Fatal, чтобы сервер не выключался сразу
	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Printf("Server failed: %v", err)
	}
}

// --- ТЕЛЕГРАМ БОТ ---
func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		return
	}
	apiURL := "https://api.telegram.org/bot" + token + "/"
	offset := 0

	for {
		resp, err := http.Get(fmt.Sprintf("%sgetUpdates?offset=%d&timeout=20", apiURL, offset))
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		var updates struct {
			Result []struct {
				UpdateID int `json:"update_id"`
				Message  struct {
					Chat struct{ ID int64 }         `json:"chat"`
					Text string                     `json:"text"`
					From struct{ FirstName string } `json:"from"`
				} `json:"message"`
			} `json:"result"`
		}
		json.NewDecoder(resp.Body).Decode(&updates)
		resp.Body.Close()

		for _, u := range updates.Result {
			if strings.HasPrefix(u.Message.Text, "/start") {
				code := fmt.Sprintf("%06d", rand.Intn(1000000))
				_, err := db.Exec(`
					INSERT INTO appointments (tg_id, patient_name, totp_secret) 
					VALUES ($1, $2, $3) 
					ON CONFLICT (tg_id) DO UPDATE SET totp_secret = $3`,
					u.Message.Chat.ID, u.Message.From.FirstName, code)

				if err == nil {
					http.Get(apiURL + "sendMessage?chat_id=" + fmt.Sprint(u.Message.Chat.ID) + "&text=Код: " + code)
				}
			}
			offset = u.UpdateID + 1
		}
	}
}

// --- ЛОГИКА САЙТА ---
func handleRoot(w http.ResponseWriter, r *http.Request) {
	cID, _ := r.Cookie("user_id")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "<h1>HealthOS</h1><a href='%s'>1. Код в Telegram</a><br><form action='/verify-otp' method='POST'><input name='otp'><button type='submit'>Войти</button></form>", BOT_LINK)
		return
	}

	role, _ := r.Cookie("user_role")
	name, _ := r.Cookie("user_name")

	if role.Value == "doctor" {
		fmt.Fprintf(w, "<h2>Кабинет Доктора: %s</h2>", name.Value)
		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments WHERE tg_id != $1", MY_TG_ID)
		for rows.Next() {
			var pID int64
			var pName, pDiag string
			rows.Scan(&pID, &pName, &pDiag)
			fmt.Fprintf(w, "<div>%s: <form action='/save-diagnosis' method='POST' style='display:inline;'><input type='hidden' name='tg_id' value='%d'><input name='diagnosis' value='%s'><button type='submit'>OK</button></form></div>", pName, pID, pDiag)
		}
		rows.Close()
	} else {
		var diag string
		_ = db.QueryRow("SELECT diagnosis FROM appointments WHERE tg_id = $1", cID.Value).Scan(&diag)
		fmt.Fprintf(w, "<h2>Кабинет Пациента: %s</h2><p>Диагноз: %s</p>", name.Value, diag)
	}
	fmt.Fprint(w, "<br><a href='/logout'>Выйти</a>")
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		_, _ = db.Exec("UPDATE appointments SET diagnosis = $1 WHERE tg_id = $2", r.FormValue("diagnosis"), r.FormValue("tg_id"))
	}
	http.Redirect(w, r, "/", 302)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	var tgID int64
	var name string
	err := db.QueryRow("SELECT tg_id, patient_name FROM appointments WHERE totp_secret = $1", r.FormValue("otp")).Scan(&tgID, &name)
	if err != nil {
		fmt.Fprint(w, "Ошибка! Код не найден.")
		return
	}
	role := "patient"
	if tgID == MY_TG_ID {
		role = "doctor"
	}

	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: fmt.Sprint(tgID), Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: role, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_name", Value: name, Path: "/"})
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}
