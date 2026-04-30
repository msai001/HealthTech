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

// !!! ЗАМЕНИ НА СВОЙ ID !!!
const MY_TG_ID = 58392011
const BOT_LINK = "https://t.me/health_os_bot" // Замени на имя своего бота

var db *sql.DB

func main() {
	rand.Seed(time.Now().UnixNano())

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Connection error:", err)
	}

	// Проверяем жива ли база
	err = db.Ping()
	if err != nil {
		log.Fatal("Database is unreachable:", err)
	}

	// Инициализация таблицы без удаления (DROP убираем для стабильности)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			totp_secret TEXT,
			diagnosis TEXT DEFAULT 'Диагноз еще не поставлен'
		)`)
	if err != nil {
		log.Printf("Table init error: %v", err)
	}

	go startTelegramBot()

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/logout", handleLogout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server started on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

// --- БОТ ---
func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Println("TG Token missing")
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
					http.Get(apiURL + "sendMessage?chat_id=" + fmt.Sprint(u.Message.Chat.ID) + "&text=Ваш код HealthOS: " + code)
				}
			}
			offset = u.UpdateID + 1
		}
	}
}

// --- ИНТЕРФЕЙС ---
func handleRoot(w http.ResponseWriter, r *http.Request) {
	cID, _ := r.Cookie("user_id")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	style := `<style>
		body { font-family: 'Segoe UI', sans-serif; background: #f0f4f7; display: flex; justify-content: center; padding: 20px; }
		.card { background: white; padding: 30px; border-radius: 12px; box-shadow: 0 4px 15px rgba(0,0,0,0.1); width: 100%; max-width: 500px; text-align: center; }
		.item { border-bottom: 1px solid #eee; padding: 10px; display: flex; justify-content: space-between; align-items: center; text-align: left; }
		.btn { padding: 8px 15px; border: none; border-radius: 5px; background: #28a745; color: white; cursor: pointer; }
		input { padding: 8px; border: 1px solid #ddd; border-radius: 5px; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "%s<div class='card'><h1>HealthOS</h1><p>Авторизация через Telegram</p><a href='%s' target='_blank'>1. Получить код</a><br><br><form action='/verify-otp' method='POST'><input name='otp' placeholder='Код' required><br><br><button class='btn' type='submit'>Войти</button></form></div>", style, BOT_LINK)
		return
	}

	role, _ := r.Cookie("user_role")
	name, _ := r.Cookie("user_name")

	fmt.Fprintf(w, "%s<div class='card'>", style)
	if role.Value == "doctor" {
		fmt.Fprintf(w, "<h2 style='color:#d9534f'>👨‍⚕️ Кабинет Доктора: %s</h2><hr>", name.Value)
		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments WHERE tg_id != $1", MY_TG_ID)
		for rows.Next() {
			var pID int64
			var pName, pDiag string
			rows.Scan(&pID, &pName, &pDiag)
			fmt.Fprintf(w, `<div class="item">
				<div><b>%s</b></div>
				<form action="/save-diagnosis" method="POST">
					<input type="hidden" name="tg_id" value="%d">
					<input name="diagnosis" value="%s">
					<button class="btn" type="submit">OK</button>
				</form>
			</div>`, pName, pID, pDiag)
		}
		rows.Close()
	} else {
		var diag string
		db.QueryRow("SELECT diagnosis FROM appointments WHERE tg_id = $1", cID.Value).Scan(&diag)
		fmt.Fprintf(w, "<h2>🏥 Кабинет Пациента</h2><p>Пациент: %s</p><div style='background:#e7f3ff; padding:20px; border-radius:10px;'><b>Ваш диагноз:</b><br>%s</div>", name.Value, diag)
	}
	fmt.Fprint(w, "<br><br><a href='/logout' style='color:#666;'>Выйти</a></div>")
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		db.Exec("UPDATE appointments SET diagnosis = $1 WHERE tg_id = $2", r.FormValue("diagnosis"), r.FormValue("tg_id"))
	}
	http.Redirect(w, r, "/", 302)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	var tgID int64
	var name string
	err := db.QueryRow("SELECT tg_id, patient_name FROM appointments WHERE totp_secret = $1", r.FormValue("otp")).Scan(&tgID, &name)
	if err != nil {
		fmt.Fprint(w, "Неверный код!")
		return
	}
	role := "patient"
	if tgID == MY_TG_ID {
		role = "doctor"
	}

	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: fmt.Sprint(tgID), Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: role, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_name", Value: name, Path: "/"})
	db.Exec("UPDATE appointments SET totp_secret = '' WHERE tg_id = $1", tgID)
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}
