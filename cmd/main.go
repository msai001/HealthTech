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

	log.Printf("HealthOS v4.0 | SMTP Active | Port: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func sendOTPEmail(toEmail string, code string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")

	if from == "" || pass == "" {
		log.Println("Ошибка: EMAIL_USER или EMAIL_PASS не заданы в Render")
		return
	}

	subject := "Subject: HealthOS: Код доступа\n"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	body := fmt.Sprintf("<html><body style='font-family:sans-serif;'><h2>Ваш код: <span style='color:#2563eb'>%s</span></h2></body></html>", code)
	msg := []byte(subject + mime + body)

	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	err := smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, msg)
	if err != nil {
		log.Printf("Ошибка отправки почты: %v", err)
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
	_, _ = db.Exec(`INSERT INTO appointments (user_email, patient_name, totp_secret) 
		VALUES ($1, $2, $3) ON CONFLICT (user_email) DO UPDATE SET totp_secret = $3`,
		user.Email, user.Name, otp)

	go sendOTPEmail(user.Email, otp)
	setCookie(w, "pending_user", user.Email)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; margin:0; color:white;">
			<div style="background:#1e293b; padding:40px; border-radius:24px; text-align:center; width:100%; max-width:380px; border:1px solid #334155;">
				<div style="font-size:50px; margin-bottom:20px;">🛡️</div>
				<h2 style="margin:0 0 10px 0;">Двухфакторная проверка</h2>
				<p style="color:#94a3b8; font-size:14px; line-height:1.5;">Введите код, отправленный на вашу почту.</p>
				<form action="/verify-otp" method="POST" style="margin-top:30px;">
					<input name="otp" type="text" placeholder="000000" maxlength="6" required autofocus 
						style="width:100%; padding:15px; font-size:32px; text-align:center; border:2px solid #334155; border-radius:12px; background:#0f172a; color:#38bdf8; letter-spacing:8px; margin-bottom:20px; outline:none;">
					<button type="submit" style="width:100%; background:#2563eb; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; font-size:16px; cursor:pointer;">Подтвердить</button>
				</form>
			</div>
		</body>
	`)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		return
	}
	input := r.FormValue("otp")
	pending, err := r.Cookie("pending_user")
	if err != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	var dbOtp, name string
	_ = db.QueryRow("SELECT totp_secret, patient_name FROM appointments WHERE user_email = $1", pending.Value).Scan(&dbOtp, &name)

	if input == dbOtp && dbOtp != "" {
		role := "patient"
		if pending.Value == DOCTOR_EMAIL {
			role = "doctor"
		}

		setCookie(w, "user_email", pending.Value)
		setCookie(w, "user_role", role)
		setCookie(w, "user_name", name)
		setCookie(w, "user_otp", dbOtp)

		http.SetCookie(w, &http.Cookie{Name: "pending_user", MaxAge: -1, Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<script>alert('Код неверный!'); history.back();</script>"))
	}
}

func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, errE := r.Cookie("user_email")
	cRole, errR := r.Cookie("user_role")
	if errE != nil || errR != nil {
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
	rows, err := db.Query(query + " ORDER BY id DESC")
	if err != nil {
		return
	}
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
	keys := []string{"user_email", "user_role", "user_name", "user_otp"}
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
	fmt.Fprint(w, `
	<!DOCTYPE html>
	<html lang="ru">
	<head>
		<meta charset="UTF-8">
		<title>HealthOS | Кабинет</title>
		<link href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css" rel="stylesheet">
		<style>
			:root { --primary: #2563eb; --dark: #0f172a; }
			body { font-family: sans-serif; margin: 0; display: flex; height: 100vh; background: #f8fafc; }
			.sidebar { width: 280px; background: var(--dark); color: white; padding: 25px; display: flex; flex-direction: column; }
			.main { flex: 1; padding: 40px; overflow-y: auto; }
			.card { background: white; border-radius: 20px; padding: 25px; box-shadow: 0 4px 20px rgba(0,0,0,0.05); margin-bottom: 30px; }
			.banner { background: linear-gradient(135deg, #2563eb, #1d4ed8); color: white; padding: 25px; border-radius: 20px; margin-bottom: 30px; }
		</style>
	</head>
	<body>
		<div class="sidebar">
			<h2 style="color:#38bdf8;"><i class="fas fa-heart-pulse"></i> HealthOS</h2>
			<div style="background:#1e293b; padding:15px; border-radius:15px; margin:20px 0;">
				<div id="u-role" style="font-size:10px; color:#38bdf8; font-weight:bold;"></div>
				<div id="u-mail" style="font-size:13px; font-weight:bold; word-break:break-all;"></div>
			</div>
			<button onclick="location.href='/logout'" style="margin-top:auto; background:#ef4444; color:white; border:none; padding:15px; border-radius:12px; cursor:pointer;">Выход</button>
		</div>
		<div class="main">
			<div id="doc-panel" class="card" style="display:none;">
				<h3>Панель врача</h3>
				<input id="p-mail" placeholder="Почта пациента"> <input id="p-diag" placeholder="Диагноз">
				<button onclick="save()">ОК</button>
			</div>
			<div class="banner">
				<div style="font-size:26px; font-weight:800;">ЗАСТРАХОВАН</div>
				<div><i class="fas fa-location-dot"></i> Атырау, Поликлиника №1</div>
			</div>
			<div class="card">
				<h3>Журнал записей</h3>
				<table id="list" style="width:100%;"></table>
			</div>
		</div>
		<script>
			const get = (n) => document.cookie.match('(^|;) ?'+n+'=([^;]*)(;|$)')?. [2];
			document.getElementById('u-mail').innerText = get('user_email');
			document.getElementById('u-role').innerText = get('user_role') === 'doctor' ? 'ВРАЧ' : 'ПАЦИЕНТ';
			if(get('user_role') === 'doctor') document.getElementById('doc-panel').style.display = 'block';

			function refresh() {
				fetch('/api/data').then(r => r.json()).then(data => {
					document.getElementById('list').innerHTML = data.map(d => 
						'<tr><td>'+d.date.split('T')[0]+'</td><td>'+d.email+'</td><td>'+(d.diag || 'Ожидает')+'</td></tr>'
					).join('');
				});
			}
			function save() {
				const email = document.getElementById('p-mail').value;
				const diagnosis = document.getElementById('p-diag').value;
				fetch('/api/data', {method:'POST', body: JSON.stringify({email, diagnosis})}).then(() => refresh());
			}
			refresh();
		</script>
	</body>
	</html>
	`)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}
