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

	log.Printf("HealthOS v11.0 | Atyrau Production | Port: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func sendOTPEmail(toEmail string, code string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		log.Println("ERROR: EMAIL_USER/PASS not set")
		return
	}

	subject := "Subject: HealthOS Access Code\n"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	body := fmt.Sprintf(`
		<div style="font-family:sans-serif; border:2px solid #2563eb; padding:20px; border-radius:15px; max-width:400px;">
			<h2 style="color:#2563eb;">Твой код HealthOS</h2>
			<div style="font-size:40px; font-weight:bold; letter-spacing:8px; margin:20px 0;">%s</div>
			<p>Введите этот код, чтобы войти в систему.</p>
		</div>`, code)

	msg := []byte(subject + mime + body)
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")

	err := smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, msg)
	if err != nil {
		log.Printf("SMTP Error: %v", err)
	} else {
		log.Printf("Email sent to %s", toEmail)
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

	// Сохраняем в базу (Upsert)
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
			<div style="background:#1e293b; padding:40px; border-radius:24px; text-align:center; width:360px; border:1px solid #334155;">
				<h2 style="color:#38bdf8; margin-bottom:10px;">Проверка кода</h2>
				<p style="color:#94a3b8; font-size:14px; margin-bottom:30px;">Код отправлен на вашу почту</p>
				<form action="/verify-otp" method="POST">
					<input name="otp" type="text" inputmode="numeric" pattern="[0-9]*" maxlength="6" required autofocus 
						style="width:100%; padding:15px; font-size:36px; text-align:center; border-radius:12px; background:#0f172a; color:#38bdf8; border:2px solid #334155; margin-bottom:25px; outline:none;">
					<button type="submit" style="width:100%; background:#2563eb; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; cursor:pointer; font-size:16px;">ПОДТВЕРДИТЬ</button>
				</form>
				<p style="font-size:11px; color:#64748b; margin-top:20px;">Проверьте папку "Спам", если письмо не пришло.</p>
			</div>
		</body>
	`)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		return
	}
	input := strings.TrimSpace(r.FormValue("otp"))

	var email string
	pending, err := r.Cookie("pending_user")
	if err == nil {
		email = pending.Value
	}

	var dbOtp, name, userEmail string
	// Ищем по куке или по самому последнему обновленному юзеру
	query := "SELECT TRIM(totp_secret), patient_name, user_email FROM appointments "
	if email != "" {
		query += fmt.Sprintf("WHERE user_email = '%s'", email)
	} else {
		query += "ORDER BY id DESC LIMIT 1"
	}

	_ = db.QueryRow(query).Scan(&dbOtp, &name, &userEmail)

	log.Printf("[AUTH] Input: %s | DB: %s | Target: %s", input, dbOtp, userEmail)

	if input == dbOtp && dbOtp != "" {
		role := "patient"
		if userEmail == DOCTOR_EMAIL {
			role = "doctor"
		}

		setCookie(w, "user_email", userEmail)
		setCookie(w, "user_role", role)
		setCookie(w, "user_name", name)

		http.SetCookie(w, &http.Cookie{Name: "pending_user", MaxAge: -1, Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<script>alert('Неверный код! Проверьте последнее письмо.'); history.back();</script>"))
	}
}

func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, errE := r.Cookie("user_email")
	cRole, errR := r.Cookie("user_role")
	if errE != nil || errR != nil {
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
			list = append(list, map[string]interface{}{
				"email": email, "diag": diag, "date": date, "name": name,
			})
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
	fmt.Fprintf(w, `
	<!DOCTYPE html>
	<html lang="ru">
	<head>
		<meta charset="UTF-8">
		<title>HealthOS Dashboard</title>
		<link href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css" rel="stylesheet">
		<style>
			:root { --primary: #2563eb; --dark: #0f172a; }
			body { font-family: sans-serif; margin: 0; display: flex; height: 100vh; background: #f1f5f9; }
			.sidebar { width: 280px; background: var(--dark); color: white; padding: 30px; display: flex; flex-direction: column; }
			.main { flex: 1; padding: 40px; overflow-y: auto; }
			.card { background: white; border-radius: 20px; padding: 25px; box-shadow: 0 4px 15px rgba(0,0,0,0.05); margin-bottom: 30px; }
			.status-bar { background: linear-gradient(135deg, #2563eb, #1d4ed8); color: white; padding: 25px; border-radius: 20px; margin-bottom: 30px; }
			table { width: 100%%; border-collapse: collapse; }
			td { padding: 15px; border-bottom: 1px solid #f1f5f9; }
			input { padding: 10px; border: 1px solid #ddd; border-radius: 8px; margin-right: 10px; }
		</style>
	</head>
	<body>
		<div class="sidebar">
			<h2 style="color:#38bdf8;"><i class="fas fa-heart-pulse"></i> HealthOS</h2>
			<div style="background:#1e293b; padding:15px; border-radius:12px; margin-top:20px;">
				<div id="role-tag" style="font-size:10px; font-weight:bold; color:#38bdf8;"></div>
				<div style="font-size:13px; font-weight:bold; word-break:break-all;">%s</div>
			</div>
			<button onclick="location.href='/logout'" style="margin-top:auto; background:#ef4444; color:white; border:none; padding:15px; border-radius:12px; cursor:pointer; font-weight:bold;">ВЫЙТИ</button>
		</div>
		<div class="main">
			<div id="doctor-controls" class="card" style="display:none;">
				<h3 style="margin-top:0;">👨‍⚕️ Назначение диагноза</h3>
				<input id="target-email" placeholder="Email пациента">
				<input id="diag-text" placeholder="Диагноз">
				<button onclick="submitDiag()" style="background:var(--primary); color:white; border:none; padding:10px 20px; border-radius:8px; cursor:pointer;">ОТПРАВИТЬ</button>
			</div>
			<div class="status-bar">
				<div style="font-size:20px; font-weight:bold;">ТЕКУЩИЙ СТАТУС: АКТИВЕН</div>
				<div style="opacity:0.8;"><i class="fas fa-hospital"></i> Атырау, Городская поликлиника №1</div>
			</div>
			<div class="card">
				<h3 style="margin-top:0;">История обследований</h3>
				<table id="data-table"></table>
			</div>
		</div>
		<script>
			const getCookie = (n) => document.cookie.match('(^|;) ?'+n+'=([^;]*)(;|$)')?. [2];
			const role = getCookie('user_role');
			document.getElementById('role-tag').innerText = role === 'doctor' ? 'ГЛАВНЫЙ ВРАЧ' : 'ПАЦИЕНТ';
			if(role === 'doctor') document.getElementById('doctor-controls').style.display = 'block';

			function loadData() {
				fetch('/api/data').then(r => r.json()).then(data => {
					document.getElementById('data-table').innerHTML = data.map(item => 
						'<tr><td>'+item.date.split('T')[0]+'</td><td><b>'+item.name+'</b></td><td style="text-align:right; color:#2563eb; font-weight:bold;">'+(item.diag || 'В обработке...')+'</td></tr>'
					).join('');
				});
			}

			function submitDiag() {
				const email = document.getElementById('target-email').value;
				const diagnosis = document.getElementById('diag-text').value;
				fetch('/api/data', {
					method: 'POST',
					body: JSON.stringify({email, diagnosis})
				}).then(() => {
					document.getElementById('diag-text').value = '';
					loadData();
				});
			}
			loadData();
		</script>
	</body>
	</html>
	`, cEmail.Value)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}
