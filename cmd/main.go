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

// !!! ОБЯЗАТЕЛЬНО ЗАМЕНИ ЭТИ ДАННЫЕ !!!
const MY_TG_ID = 1739738363                               // Твой ID из @userinfobot
const BOT_LINK = "https://web.telegram.org/a/#8665739584" // Ссылка на твоего бота

var db *sql.DB

func main() {
	rand.Seed(time.Now().UnixNano())
	var err error
	dbURL := os.Getenv("DATABASE_URL")
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}

	// Пересоздаем таблицу, чтобы старые колонки от Google не мешали
	log.Println("[DB] Инициализация новой структуры таблицы...")
	_, err = db.Exec(`
		DROP TABLE IF EXISTS appointments;
		CREATE TABLE appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			totp_secret TEXT,
			user_role TEXT DEFAULT 'patient'
		)`)
	if err != nil {
		log.Fatal("Ошибка создания таблицы:", err)
	}

	go startTelegramBot()

	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/logout", handleLogout)
	http.HandleFunc("/debug", handleDebug)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("[SERVER] Запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Println("[TG ERROR] Токен бота не найден в переменных окружения!")
		return
	}
	apiURL := "https://api.telegram.org/bot" + token + "/"
	log.Println("[TG] Бот запущен и слушает обновления...")
	offset := 0

	for {
		resp, err := http.Get(fmt.Sprintf("%sgetUpdates?offset=%d&timeout=20", apiURL, offset))
		if err != nil || resp == nil {
			time.Sleep(3 * time.Second)
			continue
		}

		var updates struct {
			Ok     bool `json:"ok"`
			Result []struct {
				UpdateID int `json:"update_id"`
				Message  struct {
					Chat struct{ ID int64 } `json:"chat"`
					Text string             `json:"text"`
					From struct {
						FirstName string `json:"first_name"`
					} `json:"from"`
				} `json:"message"`
			} `json:"result"`
		}
		json.NewDecoder(resp.Body).Decode(&updates)
		resp.Body.Close()

		for _, u := range updates.Result {
			if strings.HasPrefix(u.Message.Text, "/start") {
				code := fmt.Sprintf("%06d", rand.Intn(1000000))
				tgID := u.Message.Chat.ID
				name := u.Message.From.FirstName

				log.Printf("[TG] Сообщение от %s (ID: %d). Генерирую код...", name, tgID)

				_, err := db.Exec(`
					INSERT INTO appointments (tg_id, patient_name, totp_secret) 
					VALUES ($1, $2, $3)
					ON CONFLICT (tg_id) DO UPDATE SET totp_secret = $3`,
					tgID, name, code)

				if err != nil {
					log.Printf("[TG ERROR] Не удалось сохранить код в БД: %v", err)
					msg := "Ошибка сервера. Попробуй позже."
					http.Get(apiURL + "sendMessage?chat_id=" + fmt.Sprint(tgID) + "&text=" + msg)
				} else {
					msg := fmt.Sprintf("Твой секретный код: %s", code)
					http.Get(apiURL + "sendMessage?chat_id=" + fmt.Sprint(tgID) + "&text=" + msg)
					log.Printf("[TG] Код %s успешно отправлен пользователю %d", code, tgID)
				}
			}
			offset = u.UpdateID + 1
		}
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	cID, _ := r.Cookie("user_id")
	if cID == nil || cID.Value == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `
			<body style="font-family: sans-serif; text-align: center; padding-top: 50px;">
				<h1>HealthOS</h1>
				<p>Авторизация через защищенный канал Telegram</p>
				<a href="%s" target="_blank" style="display:inline-block; padding:12px 24px; background:#0088cc; color:white; text-decoration:none; border-radius:5px; font-weight:bold;">1. Получить код в Боте</a>
				<br><br>
				<form action="/verify-otp" method="POST">
					<input name="otp" placeholder="Введите 6 цифр" style="padding:10px; width:200px; text-align:center; font-size:18px;" required>
					<br><br>
					<button type="submit" style="padding:10px 20px; cursor:pointer;">2. Войти в кабинет</button>
				</form>
			</body>
		`, BOT_LINK)
		return
	}

	role, _ := r.Cookie("user_role")
	name, _ := r.Cookie("user_name")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<div style='text-align:center;'>")
	if role.Value == "doctor" {
		fmt.Fprintf(w, "<h1 style='color: darkblue;'>🏥 ПАНЕЛЬ ДОКТОРА</h1><h2>Добро пожаловать, %s!</h2>", name.Value)
	} else {
		fmt.Fprintf(w, "<h1>Личный кабинет пациента</h1><h2>Пациент: %s</h2>", name.Value)
	}
	fmt.Fprint(w, "<br><a href='/logout' style='color:red;'>Выйти из системы</a></div>")
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}
	input := r.FormValue("otp")

	var tgID int64
	var name string
	err := db.QueryRow("SELECT tg_id, patient_name FROM appointments WHERE totp_secret = $1", input).Scan(&tgID, &name)

	if err != nil {
		log.Printf("[WEB ERROR] Попытка входа с неверным кодом: %s", input)
		fmt.Fprint(w, "<h2>Ошибка! Код неверный.</h2><a href='/'>Попробовать снова</a>")
		return
	}

	role := "patient"
	if tgID == MY_TG_ID {
		role = "doctor"
	}

	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: fmt.Sprint(tgID), Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: role, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_name", Value: name, Path: "/"})

	// Стираем код, чтобы его нельзя было использовать дважды
	db.Exec("UPDATE appointments SET totp_secret = '' WHERE tg_id = $1", tgID)

	log.Printf("[WEB] Успешный вход! ID: %d, Роль: %s", tgID, role)
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT tg_id, patient_name, totp_secret FROM appointments")
	defer rows.Close()
	fmt.Fprintln(w, "SYSTEM DEBUG (Current Users in DB):")
	for rows.Next() {
		var id int64
		var n, s string
		rows.Scan(&id, &n, &s)
		fmt.Fprintf(w, "TG_ID: %d | NAME: %s | CURRENT_CODE: %s\n", id, n, s)
	}
}
