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

// ВНИМАНИЕ: Проверь свою почту здесь
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
	http.HandleFunc("/", handleRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting v1.8.5 - Full Medical OS")
	log.Fatal(http.ListenAndServe(":"+port, nil))
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
		Email string
		Name  string
	}
	json.NewDecoder(resp.Body).Decode(&user)

	role := "patient"
	if user.Email == DOCTOR_EMAIL {
		role = "doctor"
	}

	// ГЕНЕРАЦИЯ OTP ПРИ ВХОДЕ
	otp := fmt.Sprintf("%06d", rand.Intn(1000000))

	// Сохраняем или обновляем пользователя и его последний OTP
	_, err = db.Exec(`
		INSERT INTO appointments (user_email, role, patient_name, totp_secret, appointment_date) 
		VALUES ($1, $2, $3, $4, NOW()) 
		ON CONFLICT (user_email) 
		DO UPDATE SET totp_secret = $4, appointment_date = NOW()`,
		user.Email, role, user.Name, otp)

	// АВТОСОХРАНЕНИЕ: Ставим куки на 7 дней
	setSecureCookie(w, "user_email", user.Email)
	setSecureCookie(w, "user_role", role)
	setSecureCookie(w, "user_otp", otp)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func setSecureCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   604800, // 7 дней
		HttpOnly: false,  // Чтобы JS мог прочитать для интерфейса
	})
}

func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, _ := r.Cookie("user_email")
	cRole, _ := r.Cookie("user_role")

	if r.Method == "POST" && cRole.Value == "doctor" {
		var req struct{ Email, Diagnosis string }
		json.NewDecoder(r.Body).Decode(&req)
		db.Exec("UPDATE appointments SET diagnosis = $1 WHERE user_email = $2", req.Diagnosis, req.Email)
		return
	}

	query := "SELECT id, user_email, diagnosis, appointment_date, totp_secret FROM appointments"
	if cRole.Value == "patient" {
		query += " WHERE user_email = '" + cEmail.Value + "'"
	}

	rows, _ := db.Query(query + " ORDER BY id DESC")
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

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var msg struct{ Text string }
		json.NewDecoder(r.Body).Decode(&msg)
		cEmail, _ := r.Cookie("user_email")
		db.Exec("INSERT INTO messages (sender, text) VALUES ($1, $2)", cEmail.Value, msg.Text)
	} else {
		rows, _ := db.Query("SELECT sender, text FROM messages ORDER BY id ASC")
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

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
	<!DOCTYPE html>
	<html>
	<head>
		<meta charset="UTF-8">
		<title>Health Monitoring System</title>
		<style>
			:root { --blue: #4285F4; --dark: #202124; --gray: #f8f9fa; }
			body { font-family: 'Google Sans', Arial, sans-serif; margin: 0; background: var(--gray); display: flex; height: 100vh; }
			.sidebar { width: 250px; background: white; border-right: 1px solid #ddd; padding: 20px; display: flex; flex-direction: column; }
			.main { flex: 1; padding: 40px; overflow-y: auto; }
			.card { background: white; padding: 25px; border-radius: 12px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); margin-bottom: 20px; }
			.otp-box { background: #e8f0fe; padding: 15px; border-radius: 8px; border: 1px solid var(--blue); color: var(--blue); font-weight: bold; margin-bottom: 20px; }
			table { width: 100%; border-collapse: collapse; }
			th { text-align: left; color: #5f6368; font-size: 14px; padding: 10px; border-bottom: 2px solid #eee; }
			td { padding: 15px 10px; border-bottom: 1px solid #eee; }
			.btn { background: var(--blue); color: white; padding: 10px 20px; border: none; border-radius: 6px; cursor: pointer; text-decoration: none; }
			.doctor-only { background: #fff4e5; border-left: 5px solid #ffa000; padding: 20px; border-radius: 8px; margin-bottom: 20px; }
		</style>
	</head>
	<body>
		<div class="sidebar">
			<h2 style="color:var(--blue)">HealthTech</h2>
			<hr>
			<p><b>Личный кабинет</b></p>
			<div id="user-info" style="font-size: 0.9em; color: #555;">Загрузка...</div>
			<br>
			<a href="/api/auth/google" class="btn" style="text-align:center">Войти / Обновить</a>
		</div>
		<div class="main">
			<div id="otp-display" class="otp-box" style="display:none;">
				🔐 Ваш текущий OTP код для верификации: <span id="otp-code">000000</span>
			</div>

			<div id="doctor-panel" class="doctor-only" style="display:none;">
				<h3>Панель управления (Врач)</h3>
				<input id="target-email" placeholder="Email пациента" style="padding:10px">
				<input id="diag-text" placeholder="Диагноз" style="padding:10px; width:300px">
				<button class="btn" onclick="sendDiag()">Сохранить диагноз</button>
			</div>

			<div class="card">
				<h3>Медицинская карта</h3>
				<table>
					<thead><tr><th>Дата</th><th>Пациент</th><th>Диагноз</th></tr></thead>
					<tbody id="table-body"></tbody>
				</table>
			</div>

			<div class="card">
				<h3>Чат с поддержкой</h3>
				<div id="chat" style="height:150px; overflow-y:auto; border:1px solid #eee; padding:10px; margin-bottom:10px;"></div>
				<input id="chat-msg" style="width:70%; padding:10px" placeholder="Напишите сообщение...">
				<button class="btn" onclick="sendMsg()">Отправить</button>
			</div>
		</div>

		<script>
			function getCookie(name) {
				let matches = document.cookie.match(new RegExp("(?:^|; )" + name.replace(/([\.$?*|{}\(\)\[\]\\\/\+^])/g, '\\$1') + "=([^;]*)"));
				return matches ? decodeURIComponent(matches[1]) : undefined;
			}

			const email = getCookie('user_email');
			const role = getCookie('user_role');
			const otp = getCookie('user_otp');

			if(email) {
				document.getElementById('user-info').innerText = email + ' (' + role + ')';
				if(role === 'doctor') document.getElementById('doctor-panel').style.display = 'block';
				if(otp) {
					document.getElementById('otp-display').style.display = 'block';
					document.getElementById('otp-code').innerText = otp;
				}
			}

			function refresh() {
				fetch('/api/data').then(res => res.json()).then(data => {
					document.getElementById('table-body').innerHTML = data.map(i => 
						'<tr><td>'+i.date.split('T')[0]+'</td><td>'+i.email+'</td><td><b>'+(i.diag || 'На осмотре...')+'</b></td></tr>'
					).join('');
				});
				fetch('/api/chat').then(res => res.json()).then(data => {
					document.getElementById('chat').innerHTML = data.map(m => '<p><b>'+m.sender.split('@')[0]+':</b> '+m.text+'</p>').join('');
				});
			}

			function sendDiag() {
				fetch('/api/data', {
					method: 'POST',
					body: JSON.stringify({email: document.getElementById('target-email').value, diagnosis: document.getElementById('diag-text').value})
				}).then(() => { alert('Готово!'); refresh(); });
			}

			function sendMsg() {
				fetch('/api/chat', {
					method: 'POST',
					body: JSON.stringify({text: document.getElementById('chat-msg').value})
				}).then(() => { document.getElementById('chat-msg').value = ''; refresh(); });
			}

			setInterval(refresh, 5000);
			refresh();
		</script>
	</body>
	</html>
	`)
}
