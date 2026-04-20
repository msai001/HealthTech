package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/pquerna/otp/totp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Настройка Google OAuth с короткими именами Scopes
var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes: []string{
		"openid",
		"email",
		"profile",
	},
	Endpoint: google.Endpoint,
}

var db *sql.DB

// Красивые стили для интерфейса
const sharedStyles = `
	<style>
		@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;600;700&display=swap');
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: #f1f5f9; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
		.card { background: white; padding: 40px; border-radius: 24px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); text-align: center; width: 350px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 14px; background: #10b981; color: white; width: 100%; display: block; text-decoration: none; margin-top: 15px; }
		.otp-input { font-size: 32px; text-align: center; width: 100%; padding: 10px; border: 2px solid #e2e8f0; border-radius: 12px; margin: 20px 0; outline: none; letter-spacing: 5px; }
	</style>
`

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
}

func sendEmailOTP(toEmail, code string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		log.Println("Ошибка: EMAIL_USER или EMAIL_PASS не настроены")
		return
	}

	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	msg := fmt.Sprintf("Subject: HealthTech Security Code: %s\r\n\r\nYour code is: %s", code, code)

	err := smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, []byte(msg))
	if err != nil {
		log.Printf("Ошибка отправки почты: %v", err)
	}
}

func main() {
	initDB()

	// 1. Главная страница
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state", oauth2.SetAuthURLParam("prompt", "select_account"))
		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>🌿 HealthTech</h1><a href='%s' class='btn'>Войти через Google</a></div></body></html>", sharedStyles, url)
	})

	// 2. Обработка ответа от Google
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Redirect(w, r, "/", 302)
			return
		}

		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			log.Printf("Exchange error: %v", err)
			http.Redirect(w, r, "/", 302)
			return
		}

		client := googleOAuthConfig.Client(context.Background(), token)
		resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			http.Error(w, "Failed to get user info", 500)
			return
		}
		defer resp.Body.Close()

		var userInfo struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&userInfo)

		if userInfo.Email != "" {
			http.SetCookie(w, &http.Cookie{
				Name:    "user_email",
				Value:   userInfo.Email,
				Path:    "/",
				Expires: time.Now().Add(15 * time.Minute),
			})
			http.Redirect(w, r, "/otp-verify", 302)
		} else {
			http.Redirect(w, r, "/", 302)
		}
	})

	// 3. Страница ввода кода (Генерация и отправка)
	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("user_email")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}
		email := cookie.Value

		var secret string
		// Берем последний секрет
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 ORDER BY id DESC LIMIT 1", email).Scan(&secret)

		if secret == "" {
			key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
			secret = key.Secret()
			// Очищаем старые записи перед вставкой, чтобы не было дублей
			db.Exec("DELETE FROM appointments WHERE user_email = $1", email)
			db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'User', '2026-01-01', 'System')", email, secret)
		}

		secret = strings.TrimSpace(secret)
		otpCode, _ := totp.GenerateCode(secret, time.Now())
		go sendEmailOTP(email, otpCode)

		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h2>Введите код</h2><p>Отправлено на %s</p><form action='/otp-check' method='POST'><input type='text' name='code' class='otp-input' required autofocus autocomplete='off'><button type='submit' class='btn'>Подтвердить</button></form></div></body></html>", sharedStyles, email)
	})

	// 4. Проверка введенного кода
	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("user_email")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}
		userCode := strings.TrimSpace(r.FormValue("code"))

		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 ORDER BY id DESC LIMIT 1", cookie.Value).Scan(&secret)

		secret = strings.TrimSpace(secret)

		// Skew 3 дает запас по времени в 1.5 минуты
		valid, _ := totp.ValidateCustom(userCode, secret, time.Now(), totp.ValidateOpts{
			Skew: 3, Digits: 6, Period: 30, Algorithm: 0,
		})

		if valid {
			fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>✅ Успех</h1><p>Вы вошли в систему!</p></div></body></html>", sharedStyles)
		} else {
			fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>❌ Ошибка</h1><p>Код неверный.</p><a href='/otp-verify' class='btn'>Попробовать еще раз</a></div></body></html>", sharedStyles)
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Сервер запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
