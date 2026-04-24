package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ВНИМАНИЕ: Твой email станет почтой ВРАЧА
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
	log.Printf("Starting v1.7.5 - Doctor/Patient System")
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
	if err != nil || resp == nil { // Исправление ошибки из VS Code
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	defer resp.Body.Close()

	var user struct{ Email string }
	json.NewDecoder(resp.Body).Decode(&user)

	role := "patient"
	if user.Email == DOCTOR_EMAIL {
		role = "doctor"
	}

	// Если пациента нет в базе - создаем пустую запись
	db.Exec("INSERT INTO appointments (user_email, role, patient_name) VALUES ($1, $2, 'Новый пациент') ON CONFLICT DO NOTHING", user.Email, role)

	http.SetCookie(w, &http.Cookie{Name: "user_email", Value: user.Email, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: role, Path: "/"})
	http.Redirect(w, r, "/", http.StatusSeeOther)
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

	query := "SELECT id, user_email, diagnosis, appointment_date FROM appointments"
	if cRole.Value == "patient" {
		query += " WHERE user_email = '" + cEmail.Value + "'"
	}

	rows, _ := db.Query(query + " ORDER BY id DESC")
	defer rows.Close()
	var list []map[string]interface{}
	for rows.Next() {
		var id int
		var email, diag, date string
		rows.Scan(&id, &email, &diag, &date)
		list = append(list, map[string]interface{}{"id": id, "email": email, "diag": diag, "date": date})
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
		<title>HealthTech Portal</title>
		<style>
			body { font-family: sans-serif; background: #f0f4f8; margin: 0; padding: 20px; }
			.container { max-width: 900px; margin: auto; background: white; padding: 25px; border-radius: 12px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); }
			.role-doc { background: #fff3cd; padding: 15px; border-radius: 8px; margin-bottom: 20px; border-left: 5px solid #ffc107; }
			table { width: 100%; border-collapse: collapse; margin-top: 20px; }
			th, td { padding: 12px; border-bottom: 1px solid #eee; text-align: left; }
			.chat { background: #f9f9f9; height: 150px; overflow-y: auto; border: 1px solid #ddd; padding: 10px; margin: 15px 0; border-radius: 5px; }
			input, button { padding: 10px; border-radius: 5px; border: 1px solid #ccc; }
			.btn-main { background: #4285F4; color: white; border: none; cursor: pointer; }
		</style>
	</head>
	<body>
		<div class="container">
			<div style="display:flex; justify-content:space-between; align-items:center;">
				<h1>Health Monitoring <span id="role" style="font-size:0.5em; color:#666;"></span></h1>
				<a href="/api/auth/google" style="text-decoration:none; color:#4285F4;">Сменить аккаунт</a>
			</div>

			<div id="doctor-tools" style="display:none;" class="role-doc">
				<h3>Управление диагнозами (Только для врача)</h3>
				<input id="p-email" placeholder="Email пациента">
				<input id="p-diag" style="width:40%" placeholder="Введите диагноз">
				<button class="btn-main" onclick="publish()">Опубликовать</button>
			</div>

			<h3>Ваш Личный кабинет:</h3>
			<table>
				<thead><tr><th>Дата</th><th>Аккаунт</th><th>Диагноз врача</th></tr></thead>
				<tbody id="med-data"></tbody>
			</table>

			<div style="margin-top:40px; border-top: 2px solid #eee; padding-top:20px;">
				<h3>Чат с врачом (Online)</h3>
				<div class="chat" id="chat-box">Загрузка сообщений...</div>
				<input id="msg-input" style="width:70%" placeholder="Напишите сообщение...">
				<button class="btn-main" onclick="sendMsg()">Отправить</button>
			</div>
		</div>

		<script>
			const cookies = document.cookie.split('; ').reduce((prev, current) => {
				const [name, value] = current.split('=');
				prev[name] = value;
				return prev;
			}, {});

			const role = cookies['user_role'] || 'guest';
			document.getElementById('role').innerText = '(' + role.toUpperCase() + ')';
			if(role === 'doctor') document.getElementById('doctor-tools').style.display = 'block';

			function updateUI() {
				fetch('/api/data').then(r => r.json()).then(data => {
					document.getElementById('med-data').innerHTML = data.map(i => 
						'<tr><td>'+i.date+'</td><td>'+i.email+'</td><td><b>'+(i.diag || 'В обработке...')+'</b></td></tr>'
					).join('');
				});
				fetch('/api/chat').then(r => r.json()).then(msgs => {
					document.getElementById('chat-box').innerHTML = msgs.map(m => 
						'<p><b>' + m.sender.split('@')[0] + ':</b> ' + m.text + '</p>'
					).join('');
				});
			}

			function publish() {
				fetch('/api/data', {
					method: 'POST',
					body: JSON.stringify({email: document.getElementById('p-email').value, diagnosis: document.getElementById('p-diag').value})
				}).then(() => { alert('Диагноз отправлен!'); updateUI(); });
			}

			function sendMsg() {
				const text = document.getElementById('msg-input').value;
				fetch('/api/chat', { method: 'POST', body: JSON.stringify({text}) }).then(() => {
					document.getElementById('msg-input').value = '';
					updateUI();
				});
			}

			setInterval(updateUI, 3000);
			updateUI();
		</script>
	</body>
	</html>
	`)
}
