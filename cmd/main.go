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

var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"openid", "email", "profile"},
	Endpoint:     google.Endpoint,
}

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	rand.Seed(time.Now().UnixNano())

	http.HandleFunc("/api/auth/google", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})

	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/api/data", handleData)
	http.HandleFunc("/api/chat", handleChat)

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		cookieClear(w, "user_email")
		cookieClear(w, "user_role")
		cookieClear(w, "user_otp")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	http.HandleFunc("/", handleRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting v1.9.0 - Luxury Interface & Bug Fixes")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func cookieClear(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1})
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

	// ИСПРАВЛЕНИЕ ОШИБКИ ИЗ VS CODE: Сначала проверка err, потом работа с resp
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	if resp == nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	defer resp.Body.Close()

	var user struct {
		Email string
		Name  string
	}
	json.NewDecoder(resp.Body).Decode(&user)

	role := "patient"
	if user.Email == DOCTOR_EMAIL {
		role = "doctor"
	}
	otp := fmt.Sprintf("%06d", rand.Intn(1000000))

	db.Exec(`INSERT INTO appointments (user_email, role, patient_name, totp_secret, appointment_date) 
		VALUES ($1, $2, $3, $4, NOW()) 
		ON CONFLICT (user_email) DO UPDATE SET totp_secret = $4`,
		user.Email, role, user.Name, otp)

	setCookie(w, "user_email", user.Email)
	setCookie(w, "user_role", role)
	setCookie(w, "user_otp", otp)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}

func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, _ := r.Cookie("user_email")
	cRole, _ := r.Cookie("user_role")

	if r.Method == "POST" && cRole != nil && cRole.Value == "doctor" {
		var req struct{ Email, Diagnosis string }
		json.NewDecoder(r.Body).Decode(&req)
		db.Exec("UPDATE appointments SET diagnosis = $1 WHERE user_email = $2", req.Diagnosis, req.Email)
		return
	}

	query := "SELECT id, user_email, diagnosis, appointment_date, totp_secret FROM appointments"
	if cRole != nil && cRole.Value == "patient" {
		query += " WHERE user_email = '" + cEmail.Value + "'"
	}

	rows, _ := db.Query(query + " ORDER BY id DESC")
	if rows != nil {
		defer rows.Close()
		var list []map[string]interface{}
		for rows.Next() {
			var id int
			var email, diag, date, otp string
			rows.Scan(&id, &email, &diag, &date, &otp)
			list = append(list, map[string]interface{}{"id": id, "email": email, "diag": diag, "date": date, "otp": otp})
		}
		json.NewEncoder(w).Encode(list)
	}
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var msg struct{ Text string }
		json.NewDecoder(r.Body).Decode(&msg)
		cEmail, _ := r.Cookie("user_email")
		if cEmail != nil {
			db.Exec("INSERT INTO messages (sender, text) VALUES ($1, $2)", cEmail.Value, msg.Text)
		}
	} else {
		rows, _ := db.Query("SELECT sender, text FROM messages ORDER BY id ASC")
		if rows != nil {
			defer rows.Close()
			var msgs []map[string]string
			for rows.Next() {
				var s, t string
				rows.Scan(&s, &t)
				msgs = append(msgs, map[string]string{"sender": s, "text": t})
			}
			json.NewEncoder(w).Encode(msgs)
		}
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
	<!DOCTYPE html>
	<html lang="ru">
	<head>
		<meta charset="UTF-8">
		<title>Medical Dashboard Pro</title>
		<link href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.0.0/css/all.min.css" rel="stylesheet">
		<style>
			:root { --primary: #007bff; --bg: #f4f7f6; --white: #ffffff; --text: #333; }
			body { font-family: 'Segoe UI', Roboto, sans-serif; margin: 0; background: var(--bg); display: flex; height: 100vh; overflow: hidden; }
			
			/* Sidebar Design */
			.sidebar { width: 300px; background: #1c1e21; color: white; display: flex; flex-direction: column; padding: 25px; box-shadow: 4px 0 10px rgba(0,0,0,0.1); }
			.sidebar h2 { color: var(--primary); font-size: 24px; margin-bottom: 30px; display: flex; align-items: center; gap: 10px; }
			
			.profile-card { background: #2c2f33; padding: 20px; border-radius: 15px; margin-bottom: 20px; border: 1px solid #3e4146; }
			.otp-box { background: rgba(0, 123, 255, 0.1); border: 1px dashed var(--primary); color: var(--primary); padding: 10px; border-radius: 8px; text-align: center; font-weight: bold; font-size: 1.1em; margin: 10px 0; }
			
			.nav-link { color: #adb5bd; text-decoration: none; padding: 12px; border-radius: 8px; display: flex; align-items: center; gap: 12px; transition: 0.3s; margin: 5px 0; }
			.nav-link:hover, .nav-link.active { background: var(--primary); color: white; }
			
			/* Main Content */
			.content { flex: 1; padding: 40px; overflow-y: auto; }
			.card { background: var(--white); border-radius: 16px; padding: 30px; box-shadow: 0 4px 20px rgba(0,0,0,0.05); margin-bottom: 30px; }
			
			table { width: 100%; border-collapse: collapse; }
			th { text-align: left; padding: 15px; border-bottom: 2px solid #f0f2f5; color: #888; text-transform: uppercase; font-size: 12px; }
			td { padding: 18px 15px; border-bottom: 1px solid #f0f2f5; font-size: 14px; }
			
			.doctor-tools { background: #fff4e5; border-left: 6px solid #ffa000; padding: 20px; border-radius: 12px; margin-bottom: 30px; }
			.input-group { display: flex; gap: 10px; margin-top: 10px; }
			input { padding: 12px; border: 1px solid #ddd; border-radius: 8px; outline: none; }
			
			.btn { background: var(--primary); color: white; padding: 12px 24px; border: none; border-radius: 8px; cursor: pointer; font-weight: 600; transition: 0.2s; }
			.btn:hover { opacity: 0.9; transform: translateY(-1px); }
			.btn-logout { background: #dc3545; width: 100%; margin-top: 20px; }
			
			#chat-window { height: 250px; overflow-y: auto; background: #f9f9f9; border-radius: 12px; padding: 20px; border: 1px solid #eee; margin-bottom: 15px; }
			.msg { margin-bottom: 10px; padding: 10px; border-radius: 8px; background: white; border: 1px solid #eee; }
		</style>
	</head>
	<body>
		<div class="sidebar">
			<h2><i class="fas fa-heartbeat"></i> HealthOS</h2>
			
			<div id="user-ui" style="display:none;">
				<div class="profile-card">
					<div style="font-size: 12px; color: #999; margin-bottom: 5px;">ЛИЧНЫЙ КАБИНЕТ</div>
					<div id="profile-email" style="font-weight: bold; margin-bottom: 5px; overflow: hidden; text-overflow: ellipsis;"></div>
					<div id="profile-role" style="color: var(--primary); font-size: 13px;"></div>
					<div class="otp-box">OTP: <span id="profile-otp">000000</span></div>
					<a href="/logout" class="btn btn-logout" style="text-align:center; display:block; text-decoration:none;">Выход</a>
				</div>
			</div>

			<div id="guest-ui">
				<p style="color: #888; font-size: 14px;">Авторизуйтесь для доступа</p>
				<a href="/api/auth/google" class="btn" style="display:block; text-align:center; text-decoration:none;"><i class="fab fa-google"></i> Войти</a>
			</div>

			<nav style="margin-top:20px;">
				<a href="#" class="nav-link active"><i class="fas fa-columns"></i> Обзор</a>
				<a href="#" class="nav-link"><i class="fas fa-user-md"></i> Мои врачи</a>
				<a href="#" class="nav-link"><i class="fas fa-file-invoice-dollar"></i> Счета</a>
			</nav>
		</div>

		<div class="content">
			<div id="doctor-panel" class="doctor-tools" style="display:none;">
				<h3><i class="fas fa-user-shield"></i> Панель врача</h3>
				<div class="input-group">
					<input id="p-email" placeholder="Email пациента" style="flex:1">
					<input id="p-diag" placeholder="Заключение/Диагноз" style="flex:2">
					<button class="btn" onclick="saveData()">Обновить</button>
				</div>
			</div>

			<div class="card">
				<h3><i class="fas fa-clipboard-list"></i> История обращений</h3>
				<table>
					<thead><tr><th>Дата</th><th>Пациент</th><th>Заключение</th></tr></thead>
					<tbody id="data-table"></tbody>
				</table>
			</div>

			<div class="card">
				<h3><i class="fas fa-comment-medical"></i> Сообщения</h3>
				<div id="chat-window">Нужна авторизация...</div>
				<div class="input-group">
					<input id="chat-in" style="flex:1" placeholder="Введите сообщение...">
					<button class="btn" onclick="sendMsg()"><i class="fas fa-paper-plane"></i></button>
				</div>
			</div>
		</div>

		<script>
			function getC(name) {
				let matches = document.cookie.match(new RegExp("(?:^|; )" + name.replace(/([\.$?*|{}\(\)\[\]\\\/\+^])/g, '\\$1') + "=([^;]*)"));
				return matches ? decodeURIComponent(matches[1]) : undefined;
			}

			const email = getC('user_email');
			const role = getC('user_role');
			const otp = getC('user_otp');

			if(email) {
				document.getElementById('guest-ui').style.display = 'none';
				document.getElementById('user-ui').style.display = 'block';
				document.getElementById('profile-email').innerText = email;
				document.getElementById('profile-role').innerText = role === 'doctor' ? '👨‍⚕️ Главный врач' : '👤 Пациент';
				document.getElementById('profile-otp').innerText = otp || '------';
				if(role === 'doctor') document.getElementById('doctor-panel').style.display = 'block';
			}

			function refresh() {
				fetch('/api/data').then(res => res.json()).then(data => {
					document.getElementById('data-table').innerHTML = data.map(i => 
						'<tr><td>'+i.date.split('T')[0]+'</td><td>'+i.email+'</td><td><b>'+(i.diag || 'На рассмотрении')+'</b></td></tr>'
					).join('');
				});
				fetch('/api/chat').then(res => res.json()).then(msgs => {
					document.getElementById('chat-window').innerHTML = msgs.map(m => '<div class="msg"><b>'+m.sender.split('@')[0]+':</b> '+m.text+'</div>').join('');
				});
			}

			function saveData() {
				fetch('/api/data', {
					method: 'POST',
					body: JSON.stringify({email: document.getElementById('p-email').value, diagnosis: document.getElementById('p-diag').value})
				}).then(() => { alert('Запись обновлена!'); refresh(); });
			}

			function sendMsg() {
				if(!email) return alert('Войдите в систему');
				fetch('/api/chat', {
					method: 'POST',
					body: JSON.stringify({text: document.getElementById('chat-in').value})
				}).then(() => { document.getElementById('chat-in').value = ''; refresh(); });
			}

			if(email) { setInterval(refresh, 4000); refresh(); }
		</script>
	</body>
	</html>
	`)
}
