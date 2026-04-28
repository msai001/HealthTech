package main

import (
	"context"
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
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const DOCTOR_EMAIL = "nur.mahambet2005@gmail.com"

var (
	db                *sql.DB
	googleOAuthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  "https://healthtech-1.onrender.com/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
)

func main() {
	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("Критическая ошибка БД:", err)
	}

	rand.Seed(time.Now().UnixNano())

	go startTelegramBot()

	http.HandleFunc("/api/auth/google", handleLogin)
	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/api/data", handleData)
	http.HandleFunc("/logout", handleLogout)
	http.HandleFunc("/", handleRoot)

	// НОВЫЙ МАРШРУТ: СЕКРЕТНЫЙ РЕНТГЕН БАЗЫ
	http.HandleFunc("/debug", handleDebug)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("HealthOS v21.0 | Fix SQL & Debugger | Порт: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		return
	}
	apiURL := "https://api.telegram.org/bot" + token + "/"
	offset := 0

	for {
		resp, err := http.Get(fmt.Sprintf("%sgetUpdates?offset=%d&timeout=15", apiURL, offset))
		if err != nil || resp == nil {
			time.Sleep(3 * time.Second)
			continue
		}

		var updates struct {
			Ok     bool `json:"ok"`
			Result []struct {
				UpdateID int `json:"update_id"`
				Message  struct {
					Chat struct{ ID int } `json:"chat"`
					Text string           `json:"text"`
				} `json:"message"`
			} `json:"result"`
		}
		json.NewDecoder(resp.Body).Decode(&updates)
		resp.Body.Close()

		for _, u := range updates.Result {
			if strings.HasPrefix(u.Message.Text, "/start") {
				code := fmt.Sprintf("%06d", rand.Intn(1000000))

				// res, err := db.Exec(`INSERT INTO appointments (totp_secret, patient_name, appointment_date, doctor_name, user_email)
				// 	VALUES ($1, $2, $3, $4, $5)`, code, "", time.Now(), "Dr. Smith", "")
				res, err := db.Exec(`UPDATE appointments SET totp_secret = $1 WHERE user_email = (SELECT user_email FROM appointments ORDER BY id DESC LIMIT 1)`, code)

				if err != nil {
					log.Printf("[TG ERROR] Ошибка БД: %v", err)
				}

				rows, _ := res.RowsAffected()
				log.Printf("[TG] Сгенерирован код %s. Обновлено строк: %d", code, rows)

				http.Get(apiURL + "sendMessage?chat_id=" + fmt.Sprint(u.Message.Chat.ID) + "&text=Твой код HealthOS: " + code)
			}
			offset = u.UpdateID + 1
		}
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	botLink := "https://web.telegram.org/a/#8665739584"

	fmt.Fprintf(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; margin:0; color:white;">
			<div style="background:#1e293b; padding:40px; border-radius:24px; text-align:center; width:350px;">
				<h1 style="color:#38bdf8;">HealthOS</h1>
				<a href="%s" style="display:block; background:white; color:#0f172a; padding:15px; border-radius:12px; text-decoration:none; font-weight:bold; margin-bottom:20px;">
				   Войти через Google
				</a>
				<p style="font-size:12px; color:#64748b;">Затем напишите /start боту:<br><a href="%s" style="color:#38bdf8;">Перейти в Telegram</a></p>
			</div>
		</body>`, googleOAuthConfig.AuthCodeURL("state"), botLink)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	token, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Redirect(w, r, "/api/auth/google", 302)
		return
	}

	client := googleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil || resp == nil {
		http.Redirect(w, r, "/api/auth/google", 302)
		return
	}
	defer resp.Body.Close()

	var user struct{ Email, Name string }
	json.NewDecoder(resp.Body).Decode(&user)

	// ИСПРАВЛЕНИЕ: Безопасное сохранение без зависимости от UNIQUE Constraint
	var count int
	db.QueryRow("SELECT COUNT(id) FROM appointments WHERE user_email = $1", user.Email).Scan(&count)

	if count > 0 {
		_, err = db.Exec("UPDATE appointments SET patient_name = $2, totp_secret = '' WHERE user_email = $1", user.Email, user.Name)
	} else {
		_, err = db.Exec("INSERT INTO appointments (user_email, patient_name, totp_secret, appointment_date, doctor_name) VALUES ($1, $2, '', $3, $4)", user.Email, user.Name, time.Now(), "Dr. Smith")
	}
	if err != nil {
		log.Printf("[DB ERROR] Ошибка записи юзера: %v", err)
	}

	http.SetCookie(w, &http.Cookie{Name: "pending_user", Value: user.Email, Path: "/", MaxAge: 600})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; color:white; margin:0;">
			<form action="/verify-otp" method="POST" style="background:#1e293b; padding:40px; border-radius:24px; text-align:center;">
				<h2>Введите код</h2>
				<input name="otp" type="text" maxlength="6" required autofocus style="width:100%%; padding:15px; font-size:32px; text-align:center; border-radius:12px; margin-bottom:20px; border:none;">
				<button type="submit" style="width:100%%; background:#2563eb; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; cursor:pointer;">ВОЙТИ</button>
			</form>
		</body>`)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	input := strings.TrimSpace(r.FormValue("otp"))
	pending, err := r.Cookie("pending_user")
	var dbOtp, name, email string

	// ИСПРАВЛЕНИЕ: COALESCE защищает от ошибки, если код равен NULL
	query := "SELECT COALESCE(TRIM(totp_secret), ''), patient_name, user_email FROM appointments "
	if err == nil && pending.Value != "" {
		query += fmt.Sprintf("WHERE user_email = '%s' ", pending.Value)
	} else {
		query += "ORDER BY id DESC LIMIT 1 "
	}

	db.QueryRow(query).Scan(&dbOtp, &name, &email)
	log.Printf("[debug] dbOtp : %v", dbOtp)

	if input != "" && input == dbOtp {
		role := "patient"
		if email == DOCTOR_EMAIL {
			role = "doctor"
		}
		setCookie(w, "user_email", email)
		setCookie(w, "user_role", role)
		setCookie(w, "user_name", name)
		http.SetCookie(w, &http.Cookie{Name: "pending_user", Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/", 302)
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(fmt.Sprintf("<script>alert('Ошибка! Введено: %s, Ожидалось в базе: Makha lox %s'); history.back();</script>", input, dbOtp)))
	}
}

// РЕНТГЕН БАЗЫ ДАННЫХ (Для отладки)
func handleDebug(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, user_email, COALESCE(totp_secret, 'ПУСТО') FROM appointments ORDER BY id DESC LIMIT 15")
	if err != nil {
		w.Write([]byte("Ошибка БД: " + err.Error()))
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("--- ПОСЛЕДНИЕ ЗАПИСИ В БАЗЕ (Для разработчика) ---\n\n"))
	for rows.Next() {
		var id int
		var email, secret string
		rows.Scan(&id, &email, &secret)
		fmt.Fprintf(w, "ID: %d | Email: %s | Код: [%s]\n", id, email, secret)
	}
}

func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, _ := r.Cookie("user_email")
	cRole, _ := r.Cookie("user_role")
	if cEmail == nil {
		return
	}
	if r.Method == "POST" && cRole.Value == "doctor" {
		var req struct{ Email, Diagnosis string }
		json.NewDecoder(r.Body).Decode(&req)
		db.Exec("UPDATE appointments SET diagnosis = $1 WHERE user_email = $2", req.Diagnosis, req.Email)
		return
	}
	rows, _ := db.Query("SELECT user_email, diagnosis, appointment_date, patient_name FROM appointments ORDER BY id DESC")
	defer rows.Close()
	var list []map[string]string
	for rows.Next() {
		var e, d, dt, n string
		rows.Scan(&e, &d, &dt, &n)
		if cRole.Value == "doctor" || e == cEmail.Value {
			list = append(list, map[string]string{"email": e, "diag": d, "date": dt, "name": n})
		}
	}
	json.NewEncoder(w).Encode(list)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	cEmail, err := r.Cookie("user_email")
	if err != nil || cEmail.Value == "" {
		http.Redirect(w, r, "/api/auth/google", 302)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<html><body style="font-family:sans-serif; background:#f1f5f9; padding:40px;">
		<div style="max-width:600px; margin:auto; background:white; padding:30px; border-radius:20px; box-shadow:0 4px 6px rgba(0,0,0,0.1);">
			<h2>HealthOS Dashboard</h2><p>Юзер: <b>%s</b></p><hr><div id="list">Загрузка...</div>
			<button onclick="location.href='/logout'" style="margin-top:20px; padding:10px; background:#ef4444; color:white; border:none; border-radius:5px;">Выйти</button>
		</div>
		<script>fetch('/api/data').then(r=>r.json()).then(d => { document.getElementById('list').innerHTML = d.map(i => '<p>📅 '+i.date.split('T')[0]+': '+(i.diag||'Ожидание...')+'</p>').join(''); })</script>
	</body></html>`, cEmail.Value)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	for _, k := range []string{"user_email", "user_role", "user_name", "pending_user"} {
		http.SetCookie(w, &http.Cookie{Name: k, Value: "", Path: "/", MaxAge: -1})
	}
	http.Redirect(w, r, "/api/auth/google", 302)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}
