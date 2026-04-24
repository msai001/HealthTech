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

	log.Printf("HealthOS v3.0 | 2FA Security Active | Port: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
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

	// Генерируем 6-значный код безопасности
	otp := fmt.Sprintf("%06d", rand.Intn(1000000))

	// Сохраняем код в БД для этого email
	_, _ = db.Exec(`INSERT INTO appointments (user_email, patient_name, totp_secret) 
		VALUES ($1, $2, $3) 
		ON CONFLICT (user_email) DO UPDATE SET totp_secret = $3`,
		user.Email, user.Name, otp)

	// ВЫВОДИМ КОД В ЛОГИ (Проверь вкладку Logs на Render!)
	log.Printf("--- SECURITY ALERT: LOGIN CODE FOR %s IS [%s] ---", user.Email, otp)

	// Устанавливаем временную куку ожидания
	setCookie(w, "pending_user", user.Email)
	setCookie(w, "temp_name", user.Name)

	// Страница ввода кода (чистый HTML)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; margin:0; color:white;">
			<div style="background:#1e293b; padding:40px; border-radius:24px; box-shadow:0 20px 50px rgba(0,0,0,0.3); text-align:center; width:100%; max-width:380px; border:1px solid #334155;">
				<div style="font-size:50px; margin-bottom:20px;">🛡️</div>
				<h2 style="margin:0 0 10px 0;">Двухфакторная проверка</h2>
				<p style="color:#94a3b8; font-size:14px; line-height:1.5;">Введите 6-значный код, отправленный на вашу систему безопасности HealthOS.</p>
				<form action="/verify-otp" method="POST" style="margin-top:30px;">
					<input name="otp" type="text" placeholder="000000" maxlength="6" required autofocus 
						style="width:100%; padding:15px; font-size:32px; text-align:center; border:2px solid #334155; border-radius:12px; background:#0f172a; color:#38bdf8; letter-spacing:8px; margin-bottom:20px; outline:none;">
					<button type="submit" style="width:100%; background:#2563eb; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; font-size:16px; cursor:pointer; transition:0.2s;">
						Подтвердить и войти
					</button>
				</form>
				<div style="margin-top:20px; font-size:12px; color:#64748b;">Проверьте логи сервера для получения кода</div>
			</div>
		</body>
	`)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		return
	}

	inputOtp := r.FormValue("otp")
	pending, err := r.Cookie("pending_user")
	if err != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	var dbOtp string
	_ = db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1", pending.Value).Scan(&dbOtp)

	if inputOtp == dbOtp && dbOtp != "" {
		// Код верный — пускаем!
		role := "patient"
		if pending.Value == DOCTOR_EMAIL {
			role = "doctor"
		}
		setCookie(w, "user_email", pending.Value)
		setCookie(w, "user_role", role)
		setCookie(w, "user_otp", dbOtp)
		// Очистка временных кук
		http.SetCookie(w, &http.Cookie{Name: "pending_user", MaxAge: -1, Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<script>alert('Неверный код!'); history.back();</script>"))
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
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			_, _ = db.Exec("UPDATE appointments SET diagnosis = $1 WHERE user_email = $2", req.Diagnosis, req.Email)
		}
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
			"id":    id,
			"email": email,
			"diag":  diag,
			"date":  date,
			"name":  name,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	keys := []string{"user_email", "user_role", "user_otp", "pending_user"}
	for _, k := range keys {
		http.SetCookie(w, &http.Cookie{Name: k, Value: "", Path: "/", MaxAge: -1})
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Заменяем handleLogout выше на корректную версию без неиспользуемых параметров
func handleLogoutFixed(w http.ResponseWriter, r *http.Request) {
	keys := []string{"user_email", "user_role", "user_otp", "pending_user"}
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

	// Здесь твой красивый интерфейс из image_a2adfb.png
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
	<!DOCTYPE html>
	<html lang="ru">
	<head>
		<meta charset="UTF-8">
		<title>HealthOS | Главная</title>
		<link href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css" rel="stylesheet">
		<style>
			:root { --primary: #2563eb; --dark: #0f172a; --sidebar: #1e293b; }
			body { font-family: 'Inter', sans-serif; margin: 0; display: flex; height: 100vh; background: #f8fafc; }
			.sidebar { width: 280px; background: var(--dark); color: white; padding: 25px; display: flex; flex-direction: column; }
			.main { flex: 1; padding: 40px; overflow-y: auto; }
			.nav-item { padding: 12px 15px; border-radius: 10px; cursor: pointer; display: flex; align-items: center; gap: 12px; margin-bottom: 5px; color: #94a3b8; transition: 0.3s; }
			.nav-item.active { background: var(--primary); color: white; }
			.card { background: white; border-radius: 20px; padding: 30px; box-shadow: 0 4px 20px rgba(0,0,0,0.05); margin-bottom: 30px; }
			.doctor-panel { background: #fff7ed; border: 1px solid #ffedd5; }
			.status-badge { background: #dcfce7; color: #166534; padding: 5px 12px; border-radius: 20px; font-weight: bold; font-size: 12px; }
		</style>
	</head>
	<body>
		<div class="sidebar">
			<h2 style="color:#38bdf8; display:flex; align-items:center; gap:10px;"><i class="fas fa-heart-pulse"></i> HealthOS</h2>
			<div style="background:#1e293b; padding:15px; border-radius:15px; margin:20px 0; border:1px solid #334155;">
				<div id="u-role" style="font-size:10px; color:#38bdf8; font-weight:bold; margin-bottom:5px;"></div>
				<div id="u-mail" style="font-size:13px; font-weight:bold; word-break:break-all;"></div>
				<div id="u-otp" style="margin-top:15px; background:#0f172a; padding:10px; border-radius:8px; text-align:center; color:#38bdf8; font-family:monospace; font-size:18px; border:1px dashed #2563eb;"></div>
			</div>
			<div class="nav-item active"><i class="fas fa-th-large"></i> Обзор</div>
			<div class="nav-item"><i class="fas fa-user-doctor"></i> Мои врачи</div>
			<div class="nav-item"><i class="fas fa-file-invoice-dollar"></i> Счета</div>
			<button onclick="location.href='/logout'" style="margin-top:auto; background:#ef4444; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; cursor:pointer;">Выход</button>
		</div>
		<div class="main">
			<div id="doc-area" class="card doctor-panel" style="display:none;">
				<h3 style="margin-top:0"><i class="fas fa-user-md"></i> Панель врача</h3>
				<div style="display:flex; gap:15px;">
					<input id="p-email" placeholder="Почта пациента" style="flex:1; padding:12px; border-radius:10px; border:1px solid #ddd;">
					<input id="p-diag" placeholder="Диагноз" style="flex:2; padding:12px; border-radius:10px; border:1px solid #ddd;">
					<button onclick="update()" style="background:var(--primary); color:white; border:none; padding:0 30px; border-radius:10px; font-weight:bold; cursor:pointer;">Обновить</button>
				</div>
			</div>
			<div class="card">
				<h3><i class="fas fa-clock-rotate-left"></i> История обращений</h3>
				<table style="width:100%; border-collapse:collapse;">
					<thead><tr style="text-align:left; color:#94a3b8; font-size:12px; text-transform:uppercase;">
						<th style="padding-bottom:15px;">Дата</th><th style="padding-bottom:15px;">Пациент</th><th style="padding-bottom:15px;">Заключение</th>
					</tr></thead>
					<tbody id="list-body"></tbody>
				</table>
			</div>
		</div>
		<script>
			const getC = (n) => (document.cookie.match('(^|;) ?'+n+'=([^;]*)(;|$)') || [])[2];
			const email = getC('user_email'), role = getC('user_role'), otp = getC('user_otp');
			
			document.getElementById('u-mail').innerText = email;
			document.getElementById('u-role').innerText = role === 'doctor' ? 'ГЛАВНЫЙ ВРАЧ' : 'ПАЦИЕНТ';
			document.getElementById('u-otp').innerText = 'OTP: ' + otp;
			if(role === 'doctor') document.getElementById('doc-area').style.display = 'block';

			function load() {
				fetch('/api/data').then(r => r.json()).then(data => {
					document.getElementById('list-body').innerHTML = data.map(d =>
						'<tr style="border-top:1px solid #f1f5f9;">' +
							'<td style="padding:15px 0; font-size:14px; color:#64748b;">' + d.date.split('T')[0] + '</td>' +
							'<td style="padding:15px 0; font-weight:500;">' + d.email + '</td>' +
							'<td style="padding:15px 0;"><span class="' + (d.diag ? 'status-badge' : '') + '" style="background:' + (d.diag ? '#dcfce7':'#f1f5f9') + '; color:' + (d.diag ? '#166534':'#64748b') + '; padding:5px 12px; border-radius:20px; font-size:12px;">' + (d.diag || 'На рассмотрении') + '</span></td>' +
						'</tr>'
					).join('');
				});
			}
			function update() {
				const email = document.getElementById('p-email').value;
				const diagnosis = document.getElementById('p-diag').value;
				fetch('/api/data', {method:'POST', body: JSON.stringify({email, diagnosis})}).then(() => { alert('Успешно'); load(); });
			}
			load();
			setInterval(load, 5000);
		</script>
	</body>
	</html>
	`)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}
