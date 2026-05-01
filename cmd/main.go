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

// !!! ОБЯЗАТЕЛЬНО ПРОВЕРЬ ЭТИ ДАННЫЕ !!!
const MY_TG_ID = 58392011                     // Твой ID
const BOT_LINK = "https://t.me/health_os_bot" // Ссылка на бота

var db *sql.DB

func main() {
	rand.Seed(time.Now().UnixNano())

	dbURL := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("[DB ERROR] Ошибка открытия: %v", err)
	}

	// Инициализация таблицы
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			totp_secret TEXT,
			diagnosis TEXT DEFAULT 'Диагноз еще не поставлен'
		)`)
	if err != nil {
		log.Printf("[DB ERROR] Ошибка создания таблицы: %v", err)
	}

	// Запуск бота в отдельном потоке
	go startTelegramBot()

	// Маршруты сервера
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/logout", handleLogout)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("[SERVER] Запуск на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Println("[TG ERROR] Токен не найден в Environment Variables!")
		return
	}
	apiURL := "https://api.telegram.org/bot" + token + "/"
	offset := 0

	log.Println("[TG] Бот начал опрос обновлений...")

	for {
		resp, err := http.Get(fmt.Sprintf("%sgetUpdates?offset=%d&timeout=15", apiURL, offset))
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

		json.NewDecoder(resp.Body).Decode(&updates)
		resp.Body.Close()

		for _, u := range updates.Result {
			if strings.HasPrefix(u.Message.Text, "/start") {
				tgID := u.Message.Chat.ID
				name := u.Message.From.FirstName
				code := fmt.Sprintf("%06d", rand.Intn(1000000))

				log.Printf("[TG] Получен /start от %s (ID: %d)", name, tgID)

				// Сохраняем в БД
				_, dbErr := db.Exec(`
					INSERT INTO appointments (tg_id, patient_name, totp_secret) 
					VALUES ($1, $2, $3) 
					ON CONFLICT (tg_id) DO UPDATE SET totp_secret = $3`,
					tgID, name, code)

				if dbErr != nil {
					log.Printf("[DB ERROR] Не удалось сохранить код: %v", dbErr)
					continue
				}

				// Формируем URL именно для отправки сообщения
				sendURL := fmt.Sprintf("%ssendMessage?chat_id=%d&text=Ваш код HealthOS: %s", apiURL, tgID, code)

				// Выполняем запрос
				resp, err := http.Get(sendURL)
				if err != nil {
					log.Printf("[TG ERROR] Ошибка сети: %v", err)
				} else {
					// Важно закрыть тело ответа, чтобы не забивать память
					resp.Body.Close()
					log.Printf("[TG] Код успешно отправлен в чат %d", tgID)
				}
			}
		}
	}
}

// --- ВЕБ ИНТЕРФЕЙС ---

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")

	style := `
	<style>
		body { font-family: 'Segoe UI', sans-serif; background: #f4f7f9; text-align: center; padding-top: 50px; }
		.card { background: white; display: inline-block; padding: 40px; border-radius: 15px; box-shadow: 0 4px 15px rgba(0,0,0,0.1); width: 350px; }
		input { width: 100%; padding: 12px; margin: 15px 0; border: 1px solid #ddd; border-radius: 8px; text-align: center; font-size: 18px; box-sizing: border-box; }
		button { width: 100%; padding: 12px; background: #28a745; color: white; border: none; border-radius: 8px; font-weight: bold; cursor: pointer; }
		.btn-tg { color: #0088cc; text-decoration: none; font-weight: bold; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "%s<div class='card'><h1>HealthOS</h1><p>Введите код из Telegram</p><a href='%s' target='_blank' class='btn-tg'>Открыть бота</a><form action='/verify-otp' method='POST'><input name='otp' placeholder='000000' maxlength='6' required><button type='submit'>Войти</button></form></div>", style, BOT_LINK)
		return
	}

	role, _ := r.Cookie("user_role")
	name, _ := r.Cookie("user_name")

	fmt.Fprintf(w, "%s<div class='card'>", style)
	if role != nil && role.Value == "doctor" {
		fmt.Fprintf(w, "<h2 style='color:#d9534f'>👨‍⚕️ Кабинет Доктора</h2><p>Привет, %s</p><hr>", name.Value)
		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments WHERE tg_id != $1", MY_TG_ID)
		if rows != nil {
			for rows.Next() {
				var pID int64
				var pName, pDiag string
				rows.Scan(&pID, &pName, &pDiag)
				fmt.Fprintf(w, "<div style='text-align:left; margin-bottom:15px;'><b>%s</b><form action='/save-diagnosis' method='POST'><input type='hidden' name='tg_id' value='%d'><input name='diagnosis' value='%s' style='font-size:14px; padding:5px;'><button style='padding:5px; font-size:12px;'>Обновить</button></form></div>", pName, pID, pDiag)
			}
			rows.Close()
		}
	} else {
		var diag string
		db.QueryRow("SELECT diagnosis FROM appointments WHERE tg_id = $1", cID.Value).Scan(&diag)
		fmt.Fprintf(w, "<h2>🏥 Моя Карта</h2><p>Пациент: %s</p><div style='background:#eefbff; padding:15px; border-radius:10px; text-align:left;'><b>Диагноз:</b><br>%s</div>", name.Value, diag)
	}
	fmt.Fprint(w, "<br><a href='/logout' style='color:#999; font-size:12px;'>Выйти</a></div>")
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	otp := r.FormValue("otp")
	var tgID int64
	var name string
	err := db.QueryRow("SELECT tg_id, patient_name FROM appointments WHERE totp_secret = $1", otp).Scan(&tgID, &name)

	if err != nil {
		fmt.Fprint(w, "<h1>Ошибка</h1><p>Неверный код!</p><a href='/'>Назад</a>")
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

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	db.Exec("UPDATE appointments SET diagnosis = $1 WHERE tg_id = $2", r.FormValue("diagnosis"), r.FormValue("tg_id"))
	http.Redirect(w, r, "/", 302)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}
