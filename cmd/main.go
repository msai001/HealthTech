package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/smtp"
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
		log.Fatal(err)
	}

	rand.Seed(time.Now().UnixNano())

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

	log.Printf("HealthOS v5.0 | Extreme OTP Fix | Port: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func sendOTPEmail(toEmail string, code string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")

	if from == "" || pass == "" {
		log.Println("CRITICAL: EMAIL_USER or EMAIL_PASS not set in Render Environment")
		return
	}

	subject := "Subject: HealthOS Verification Code\n"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	body := fmt.Sprintf("<html><body style='font-family:sans-serif;'><h2>Код подтверждения: <span style='color:#2563eb; font-size:32px;'>%s</span></h2></body></html>", code)
	msg := []byte(subject + mime + body)

	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	err := smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, msg)
	if err != nil {
		log.Printf("SMTP Error: %v", err)
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	url := googleOAuthConfig.AuthCodeURL("state", oauth2.SetAuthURLParam("prompt", "select_account"))
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	token, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	client := googleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil || resp == nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	defer resp.Body.Close()

	var user struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&user)

	otp := fmt.Sprintf("%06d", rand.Intn(1000000))

	// Сохраняем в БД, используя TRIM для предотвращения проблем с пробелами в будущем
	_, err = db.Exec(`INSERT INTO appointments (user_email, patient_name, totp_secret) 
		VALUES ($1, $2, $3) ON CONFLICT (user_email) DO UPDATE SET totp_secret = $3`,
		user.Email, user.Name, otp)

	if err != nil {
		log.Printf("DB Insert Error: %v", err)
	}

	go sendOTPEmail(user.Email, otp)

	cookie := &http.Cookie{Name: "pending_user", Value: user.Email, Path: "/", MaxAge: 300}
	http.SetCookie(w, cookie)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; margin:0; color:white;">
			<div style="background:#1e293b; padding:40px; border-radius:24px; text-align:center; width:360px; border:1px solid #334155; box-shadow: 0 25px 50px -12px rgba(0,0,0,0.5);">
				<h2 style="margin-bottom:10px;">🛡️ Проверка</h2>
				<p style="color:#94a3b8; font-size:14px;">Введите 6 цифр из письма</p>
				<form action="/verify-otp" method="POST" style="margin-top:20px;">
					<input name="otp" type="text" pattern="\d*" inputmode="numeric" maxlength="6" required autofocus 
						style="width:100%; padding:15px; font-size:32px; text-align:center; border:2px solid #334155; border-radius:12px; background:#0f172a; color:#38bdf8; letter-spacing:5px; margin-bottom:20px; outline:none;">
					<button type="submit" style="width:100%; background:#2563eb; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; cursor:pointer; font-size:16px;">Войти</button>
				</form>
				<p style="font-size:11px; color:#64748b; margin-top:20px;">Не пришел код? Проверьте папку Спам.</p>
			</div>
		</body>
	`)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		return
	}

	input := strings.TrimSpace(r.FormValue("otp"))
	pending, err := r.Cookie("pending_user")
	if err != nil {
		log.Println("Cookie 'pending_user' missing")
		http.Redirect(w, r, "/api/auth/google", http.StatusSeeOther)
		return
	}

	var dbOtp, name string
	err = db.QueryRow("SELECT totp_secret, patient_name FROM appointments WHERE user_email = $1", pending.Value).Scan(&dbOtp, &name)
	if err != nil {
		log.Printf("DB Fetch Error: %v", err)
		http.Redirect(w, r, "/api/auth/google", http.StatusSeeOther)
		return
	}

	// Очищаем код из базы от любых невидимых символов/пробелов
	dbOtp = strings.TrimSpace(dbOtp)

	// ЛОГИ - ЭТО САМОЕ ВАЖНОЕ. Проверь их в Render!
	log.Printf("[SECURITY] Login Attempt | User: %s | Input: '%s' | DB: '%s'", pending.Value, input, dbOtp)

	if input == dbOtp && dbOtp != "" {
		log.Printf("[SUCCESS] User %s authenticated", pending.Value)
		role := "patient"
		if pending.Value == DOCTOR_EMAIL {
			role = "doctor"
		}

		setCookie(w, "user_email", pending.Value)
		setCookie(w, "user_role", role)
		setCookie(w, "user_name", name)

		// Удаляем временную куку
		http.SetCookie(w, &http.Cookie{Name: "pending_user", MaxAge: -1, Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	} else {
		log.Printf("[FAILURE] OTP mismatch for %s", pending.Value)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<script>alert('Неверный код! Введите код из САМОГО ПОСЛЕДНЕГО письма.'); history.back();</script>"))
	}
}

func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, errE := r.Cookie("user_email")
	cRole, errR := r.Cookie("user_role")
	if errE != nil || errR != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if r.Method == "POST" && cRole.Value == "doctor" {
		var req struct {
			Email     string `json:"email"`
			Diagnosis string `json:"diagnosis"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		_, _ = db.Exec("UPDATE appointments SET diagnosis = $1 WHERE user_email = $2", req.Diagnosis, req.Email)
		return
	}

	query := "SELECT id, user_email, diagnosis, appointment_date, patient_name FROM appointments"
	if cRole.Value == "patient" {
		query += fmt.Sprintf(" WHERE user_email = '%s'", cEmail.Value)
	}
	rows, _ := db.Query(query + " ORDER BY id DESC")
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int
		var email, diag, date, name string
		_ = rows.Scan(&id, &email, &diag, &date, &name)
		list = append(list, map[string]interface{}{
			"id": id, "email": email, "diag": diag, "date": date, "name": name,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	keys := []string{"user_email", "user_role", "user_name"}
	for _, k := range keys {
		http.SetCookie(w, &http.Cookie{Name: k, Value: "", Path: "/", MaxAge: -1})
	}
	http.Redirect(w, r, "/api/auth/google", http.StatusSeeOther)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	cEmail, err := r.Cookie("user_email")
	if err != nil || cEmail.Value == "" {
		http.Redirect(w, r, "/api/auth/google", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Здесь вставь свой HTML из прошлых сообщений (интерфейс HealthOS)
	fmt.Fprintf(w, "<html><body><h1>Добро пожаловать, %s</h1><a href='/logout'>Выход</a></body></html>", cEmail.Value)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}
