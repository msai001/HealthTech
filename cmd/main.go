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
		log.Fatal("DB Connect Error:", err)
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

	log.Printf("HealthOS v13.0 | Running on port: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func sendOTPEmail(toEmail string, code string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		log.Println("CRITICAL: EMAIL_USER or EMAIL_PASS is empty in Render Env")
		return
	}

	// Упрощенный формат письма для лучшей доставляемости
	msg := "From: " + from + "\n" +
		"To: " + toEmail + "\n" +
		"Subject: HealthOS Code: " + code + "\n" +
		"MIME-version: 1.0;\n" +
		"Content-Type: text/html; charset=\"UTF-8\";\n\n" +
		"<html><body><div style='border:2px solid #2563eb;padding:20px;border-radius:10px;text-align:center;'>" +
		"<h2>Ваш код входа: <span style='color:#2563eb;'>" + code + "</span></h2>" +
		"</div></body></html>"

	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")

	log.Printf("DEBUG: Отправка письма юзеру %s...", toEmail)

	err := smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, []byte(msg))
	if err != nil {
		log.Printf("!!! ОШИБКА SMTP: %v", err)
	} else {
		log.Printf("SUCCESS: Письмо успешно отправлено на %s", toEmail)
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

	_, err = db.Exec(`
		INSERT INTO appointments (user_email, patient_name, totp_secret) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (user_email) DO UPDATE SET totp_secret = $3`,
		user.Email, user.Name, otp)

	if err == nil {
		go sendOTPEmail(user.Email, otp)
	}

	http.SetCookie(w, &http.Cookie{Name: "pending_user", Value: user.Email, Path: "/", MaxAge: 600})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; margin:0; color:white;">
			<div style="background:#1e293b; padding:40px; border-radius:24px; text-align:center; width:350px; border:1px solid #334155;">
				<h2 style="color:#38bdf8;">Вход в кабинет</h2>
				<p style="color:#94a3b8;">Код отправлен на ваш Email</p>
				<form action="/verify-otp" method="POST" style="margin-top:20px;">
					<input name="otp" type="text" maxlength="6" required autofocus 
						style="width:100%; padding:15px; font-size:32px; text-align:center; border-radius:12px; background:#0f172a; color:#38bdf8; border:2px solid #334155; margin-bottom:20px;">
					<button type="submit" style="width:100%; background:#2563eb; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; cursor:pointer;">ПОДТВЕРДИТЬ</button>
				</form>
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

	var dbOtp, name, userEmail string
	query := "SELECT TRIM(totp_secret), patient_name, user_email FROM appointments "
	if err == nil {
		query += fmt.Sprintf("WHERE user_email = '%s'", pending.Value)
	} else {
		query += "ORDER BY id DESC LIMIT 1"
	}

	_ = db.QueryRow(query).Scan(&dbOtp, &name, &userEmail)

	if input == dbOtp && dbOtp != "" {
		role := "patient"
		if userEmail == DOCTOR_EMAIL {
			role = "doctor"
		}
		setCookie(w, "user_email", userEmail)
		setCookie(w, "user_role", role)
		setCookie(w, "user_name", name)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<script>alert('Неверный код!'); history.back();</script>"))
	}
}

func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, _ := r.Cookie("user_email")
	cRole, _ := r.Cookie("user_role")
	if cEmail == nil || cRole == nil {
		return
	}

	if r.Method == "POST" && cRole.Value == "doctor" {
		var req struct{ Email, Diagnosis string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		_, _ = db.Exec("UPDATE appointments SET diagnosis = $1 WHERE user_email = $2", req.Diagnosis, req.Email)
		return
	}

	rows, _ := db.Query("SELECT user_email, diagnosis, appointment_date, patient_name FROM appointments ORDER BY id DESC")
	defer rows.Close()
	var list []map[string]interface{}
	for rows.Next() {
		var email, diag, date, name string
		_ = rows.Scan(&email, &diag, &date, &name)
		if cRole.Value == "doctor" || email == cEmail.Value {
			list = append(list, map[string]interface{}{"email": email, "diag": diag, "date": date, "name": name})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	for _, k := range []string{"user_email", "user_role", "user_name"} {
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
	fmt.Fprintf(w, "<html><body><h1>Добро пожаловать, %s</h1><a href='/logout'>Выйти</a><script>location.href='/';</script></body></html>", cEmail.Value)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}
