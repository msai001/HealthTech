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

const MY_TG_ID = 58392011 // УБЕДИСЬ, ЧТО ЭТО ТВОЙ ID
const BOT_LINK = "https://t.me/ТВОЙ_БОТ"

var db *sql.DB

func main() {
	rand.Seed(time.Now().UnixNano())
	dbURL := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}

	// Добавляем колонку diagnosis, если её нет
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			tg_id BIGINT UNIQUE,
			patient_name TEXT,
			totp_secret TEXT,
			diagnosis TEXT DEFAULT 'Диагноз еще не поставлен'
		)`)
	if err != nil {
		log.Fatal(err)
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
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- БОТ ОСТАЕТСЯ ТАКИМ ЖЕ (БЕЗ ИЗМЕНЕНИЙ) ---
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
			time.Sleep(3 * time.Second)
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
				db.Exec(`INSERT INTO appointments (tg_id, patient_name, totp_secret) 
					VALUES ($1, $2, $3) ON CONFLICT (tg_id) DO UPDATE SET totp_secret = $3`,
					u.Message.Chat.ID, u.Message.From.FirstName, code)
				http.Get(apiURL + "sendMessage?chat_id=" + fmt.Sprint(u.Message.Chat.ID) + "&text=Код: " + code)
			}
			offset = u.UpdateID + 1
		}
	}
}

// --- ИНТЕРФЕЙС ---

func handleRoot(w http.ResponseWriter, r *http.Request) {
	cID, _ := r.Cookie("user_id")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	style := `
	<style>
		body { font-family: sans-serif; background: #f4f7f9; display: flex; justify-content: center; padding: 20px; }
		.card { background: white; padding: 30px; border-radius: 12px; shadow: 0 4px 10px rgba(0,0,0,0.1); width: 100%; max-width: 600px; text-align: center; }
		.patient-row { border-bottom: 1px solid #eee; padding: 15px; text-align: left; display: flex; justify-content: space-between; align-items: center; }
		input[type=text] { padding: 8px; border: 1px solid #ddd; border-radius: 4px; width: 60%; }
		.btn { padding: 8px 15px; border: none; border-radius: 4px; cursor: pointer; background: #28a745; color: white; }
		.diag-box { background: #eefbff; padding: 15px; border-radius: 8px; margin-top: 20px; border-left: 5px solid #0088cc; text-align: left; }
	</style>`

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "%s<div class='card'><h1>HealthOS</h1><a href='%s' style='color:#0088cc'>1. Получить код в Telegram</a><br><br><form action='/verify-otp' method='POST'><input name='otp' placeholder='Код'><button class='btn' type='submit'>Войти</button></form></div>", style, BOT_LINK)
		return
	}

	role, _ := r.Cookie("user_role")
	name, _ := r.Cookie("user_name")

	fmt.Fprintf(w, "%s<div class='card'>", style)

	if role.Value == "doctor" {
		fmt.Fprintf(w, "<h1 style='color:#d9534f'>👨‍⚕️ Кабинет Доктора: %s</h1><h3>Список пациентов:</h3>", name.Value)

		rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments WHERE tg_id != $1", MY_TG_ID)
		defer rows.Close()

		for rows.Next() {
			var pID int64
			var pName, pDiag string
			rows.Scan(&pID, &pName, &pDiag)
			fmt.Fprintf(w, `
				<div class="patient-row">
					<div><b>%s</b><br><small>ID: %d</small></div>
					<form action="/save-diagnosis" method="POST" style="width:70%%">
						<input type="hidden" name="tg_id" value="%d">
						<input type="text" name="diagnosis" value="%s">
						<button class="btn" type="submit">OK</button>
					</form>
				</div>`, pName, pID, pID, pDiag)
		}
	} else {
		// КАБИНЕТ ПАЦИЕНТА
		var diagnosis string
		db.QueryRow("SELECT diagnosis FROM appointments WHERE tg_id = $1", cID.Value).Scan(&diagnosis)

		fmt.Fprintf(w, "<h1>🏥 Кабинет Пациента</h1><h2>Здравствуйте, %s</h2>", name.Value)
		fmt.Fprintf(w, "<div class='diag-box'><h3>Ваш диагноз:</h3><p>%s</p></div>", diagnosis)
		fmt.Fprint(w, "<p style='color:gray; font-size:12px;'>Обновлено доктором в режиме реального времени</p>")
	}

	fmt.Fprint(w, "<br><a href='/logout' style='color:gray;'>Выйти</a></div>")
}

func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		return
	}

	tgID := r.FormValue("tg_id")
	diagnosis := r.FormValue("diagnosis")

	_, err := db.Exec("UPDATE appointments SET diagnosis = $1 WHERE tg_id = $2", diagnosis, tgID)
	if err != nil {
		log.Printf("Ошибка сохранения: %v", err)
	}

	http.Redirect(w, r, "/", 302)
}

// --- handleVerifyOTP, handleLogout и прочее остаются такими же ---
func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	input := r.FormValue("otp")
	var tgID int64
	var name string
	err := db.QueryRow("SELECT tg_id, patient_name FROM appointments WHERE totp_secret = $1", input).Scan(&tgID, &name)
	if err != nil {
		fmt.Fprint(w, "Код неверный!")
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
