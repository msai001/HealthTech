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

	// Запуск Telegram-бота в отдельном потоке
	go startTelegramBot()

	http.HandleFunc("/api/auth/google", handleLogin)
	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/api/data", handleData)
	http.HandleFunc("/logout", handleLogout)
	http.HandleFunc("/", handleRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("HealthOS v18.0 | Система запущена на порту: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- ЛОГИКА ТЕЛЕГРАМ-БОТА ---
func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Println("Ошибка: TELEGRAM_APITOKEN не найден в окружении")
		return
	}
	apiURL := "https://api.telegram.org/bot" + token + "/"
	offset := 0

	for {
		resp, err := http.Get(fmt.Sprintf("%sgetUpdates?offset=%d&timeout=10", apiURL, offset))
		if err != nil || resp == nil {
			time.Sleep(5 * time.Second)
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

				// Обновляем код для самой последней записи (той, что создалась при входе через Google)
				res, err := db.Exec("UPDATE appointments SET totp_secret = $1 WHERE id = (SELECT id FROM appointments ORDER BY id DESC LIMIT 1)", code)

				if err != nil {
					log.Printf("[TG ERROR] Не удалось обновить БД: %v", err)
				}

				rows, _ := res.RowsAffected()
				log.Printf("[TG] Код %s сгенерирован. Обновлено строк в базе: %d", code, rows)

				msg := "Ваш код подтверждения HealthOS: " + code
				http.Get(apiURL + "sendMessage?chat_id=" + fmt.Sprint(u.Message.Chat.ID) + "&text=" + msg)
			}
			offset = u.UpdateID + 1
		}
	}
}

// --- СТРАНИЦА ВХОДА ---
func handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; margin:0; color:white;">
			<div style="background:#1e293b; padding:40px; border-radius:24px; text-align:center; width:350px; border:1px solid #334155;">
				<h1 style="color:#38bdf8;">HealthOS</h1>
				<p style="color:#94a3b8; margin-bottom:30px;">Авторизация в системе</p>
				<a href="`+googleOAuthConfig.AuthCodeURL("state")+`" 
				   style="display:block; background:white; color:#0f172a; padding:15px; border-radius:12px; text-decoration:none; font-weight:bold; margin-bottom:15px;">
				   Войти через Google
				</a>
				<p style="font-size:12px; color:#64748b;">После входа напишите /start нашему боту в Telegram: <br>
				<a href="https://t.me/ТВОЙ_БОТ_БЕЗ_СОБАЧКИ" style="color:#38bdf8;">@ТВОЙ_БОТ_БЕЗ_СОБАЧКИ</a></p>
			</div>
		</body>`)
}

// --- CALLBACK ПОСЛЕ GOOGLE ---
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

	// Создаем или обновляем запись пользователя (код пока пустой)
	db.Exec("INSERT INTO appointments (user_email, patient_name) VALUES ($1, $2) ON CONFLICT (user_email) DO UPDATE SET patient_name = $2", user.Email, user.Name)

	http.SetCookie(w, &http.Cookie{Name: "pending_user", Value: user.Email, Path: "/", MaxAge: 600})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; color:white; margin:0;">
			<form action="/verify-otp" method="POST" style="background:#1e293b; padding:40px; border-radius:24px; text-align:center;">
				<h2>Введите код</h2>
				<p style="color:#94a3b8;">Получите код в Telegram-боте</p>
				<input name="otp" type="text" maxlength="6" required autofocus style="width:100%%; padding:15px; font-size:32px; text-align:center; border-radius:12px; margin-bottom:20px; border:none; background:#0f172a; color:#38bdf8;">
				<button type="submit" style="width:100%%; background:#2563eb; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; cursor:pointer;">ПОДТВЕРДИТЬ</button>
			</form>
		</body>`)
}

// --- ПРОВЕРКА КОДА ---
func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	input := strings.TrimSpace(r.FormValue("otp"))
	pending, err := r.Cookie("pending_user")
	var dbOtp, name, email string

	query := "SELECT TRIM(totp_secret), patient_name, user_email FROM appointments "
	if err == nil && pending.Value != "" {
		query += fmt.Sprintf("WHERE user_email = '%s' ", pending.Value)
	} else {
		query += "ORDER BY id DESC LIMIT 1 "
	}

	err = db.QueryRow(query).Scan(&dbOtp, &name, &email)

	log.Printf("[AUTH] Сверка: Ввод='%s', База='%s'", input, dbOtp)

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
		w.Write([]byte(fmt.Sprintf("<script>alert('Ошибка! Введено: %s, Ожидалось: %s'); history.back();</script>", input, dbOtp)))
	}
}

// --- ЛОГИКА ДАННЫХ И КАБИНЕТА ---
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
	w.Header().Set("Content-Type", "application/json")
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
		<div style="max-width:800px; margin:auto; background:white; padding:30px; border-radius:20px; box-shadow:0 10px 25px rgba(0,0,0,0.05);">
			<h1>Кабинет HealthOS</h1>
			<p>Добро пожаловать, <b>%s</b></p>
			<hr>
			<div id="data">Загрузка данных...</div>
			<br>
			<button onclick="location.href='/logout'" style="background:#ef4444; color:white; border:none; padding:10px 20px; border-radius:8px; cursor:pointer;">Выйти</button>
		</div>
		<script>
			fetch('/api/data').then(r=>r.json()).then(d => {
				document.getElementById('data').innerHTML = '<h3>Ваши обследования:</h3>' + 
				d.map(i => '<div style="padding:10px; border-bottom:11px solid #eee;"><b>'+i.date.split('T')[0]+'</b>: '+(i.diag || 'В обработке...')+'</div>').join('');
			})
		</script>
	</body></html>`, cEmail.Value)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	for _, k := range []string{"user_email", "user_role", "user_name"} {
		http.SetCookie(w, &http.Cookie{Name: k, Value: "", Path: "/", MaxAge: -1})
	}
	http.Redirect(w, r, "/api/auth/google", 302)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}
