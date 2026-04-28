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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// СТИЛИ (CSS)
	style := `
	<style>
		body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #f0f4f8; color: #333; margin: 0; display: flex; align-items: center; justify-content: center; height: 100vh; }
		.card { background: white; padding: 40px; border-radius: 15px; box-shadow: 0 10px 25px rgba(0,0,0,0.1); width: 100%; max-width: 400px; text-align: center; }
		h1 { color: #0056b3; margin-bottom: 10px; font-size: 28px; }
		p { color: #666; margin-bottom: 30px; }
		.btn-tg { display: inline-block; padding: 12px 24px; background: #0088cc; color: white; text-decoration: none; border-radius: 8px; font-weight: bold; transition: background 0.3s; margin-bottom: 20px; }
		.btn-tg:hover { background: #006699; }
		input { width: 100%; padding: 12px; margin-bottom: 20px; border: 1px solid #ddd; border-radius: 8px; font-size: 18px; text-align: center; box-sizing: border-box; }
		button { width: 100%; padding: 12px; background: #28a745; color: white; border: none; border-radius: 8px; font-size: 16px; font-weight: bold; cursor: pointer; transition: 0.3s; }
		button:hover { background: #218838; }
		.doctor-header { color: #d9534f; border-bottom: 2px solid #d9534f; padding-bottom: 10px; }
		.logout-link { display: block; margin-top: 20px; color: #888; text-decoration: none; font-size: 14px; }
		.logout-link:hover { color: #d9534f; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, `
			%s
			<div class="card">
				<h1>HealthOS</h1>
				<p>Вход в защищенную систему</p>
				<a href="%s" target="_blank" class="btn-tg">1. Получить код в Telegram</a>
				<form action="/verify-otp" method="POST">
					<input name="otp" placeholder="Код (6 цифр)" maxlength="6" required>
					<button type="submit">2. Подтвердить вход</button>
				</form>
			</div>
		`, style, BOT_LINK)
		return
	}

	role, _ := r.Cookie("user_role")
	name, _ := r.Cookie("user_name")

	fmt.Fprintf(w, "%s<div class='card'>", style)
	if role != nil && role.Value == "doctor" {
		fmt.Fprintf(w, "<h1 class='doctor-header'>🏥 Панель доктора</h1><p>Добро пожаловать в систему управления, <b>%s</b></p>", name.Value)
		fmt.Fprint(w, "<div style='text-align: left; background: #fff5f5; padding: 10px; border-radius: 5px; font-size: 14px;'>• Управление записями<br>• Доступ к базе пациентов</div>")
	} else {
		fmt.Fprintf(w, "<h1>Личный кабинет</h1><p>Пациент: <b>%s</b></p>", name.Value)
		fmt.Fprint(w, "<div style='text-align: left; background: #f0f9ff; padding: 10px; border-radius: 5px; font-size: 14px;'>• Мои анализы<br>• История посещений</div>")
	}
	fmt.Fprint(w, "<a href='/logout' class='logout-link'>Выйти из системы</a></div>")
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
