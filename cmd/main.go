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
		log.Fatal("Ошибка подключения к БД:", err)
	}

	rand.Seed(time.Now().UnixNano())

	// Запуск Telegram бота в фоне
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

	log.Printf("HealthOS v17.0 | Запущено на порту: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- ЛОГИКА ТЕЛЕГРАМ БОТА ---
func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Println("Предупреждение: TELEGRAM_APITOKEN не установлен")
		return
	}
	apiURL := "https://api.telegram.org/bot" + token + "/"
	offset := 0

	for {
		resp, err := http.Get(fmt.Sprintf("%sgetUpdates?offset=%d&timeout=20", apiURL, offset))
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
				// Привязываем код к последней записи в базе
				db.Exec("UPDATE appointments SET totp_secret = $1 WHERE id = (SELECT max(id) FROM appointments)", code)

				log.Printf("[TG] Код %s сгенерирован для чата %d", code, u.Message.Chat.ID)
				msg := "Твой код доступа HealthOS: " + code
				http.Get(apiURL + "sendMessage?chat_id=" + fmt.Sprint(u.Message.Chat.ID) + "&text=" + msg)
			}
			offset = u.UpdateID + 1
		}
	}
}

// --- ОБРАБОТЧИКИ (HANDLERS) ---
func handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; margin:0; color:white;">
			<div style="background:#1e293b; padding:40px; border-radius:24px; text-align:center; width:350px;">
				<h1 style="color:#38bdf8;">HealthOS</h1>
				<p>Система мониторинга здоровья</p>
				<a href="`+googleOAuthConfig.AuthCodeURL("state")+`" 
				   style="display:block; background:white; color:#0f172a; padding:15px; border-radius:12px; text-decoration:none; font-weight:bold; margin:20px 0;">
				   Войти через Google
				</a>
				<p style="font-size:12px; color:#64748b;">Если код не придет на почту, напишите /start нашему боту в Telegram</p>
			</div>
		</body>`)
}

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

	otp := fmt.Sprintf("%06d", rand.Intn(1000000))
	db.Exec("INSERT INTO appointments (user_email, patient_name, totp_secret) VALUES ($1, $2, $3) ON CONFLICT (user_email) DO UPDATE SET totp_secret = $3", user.Email, user.Name, otp)

	log.Printf("[AUTH] Сгенерирован код %s для %s", otp, user.Email)
	go sendOTPEmail(user.Email, otp)

	http.SetCookie(w, &http.Cookie{Name: "pending_user", Value: user.Email, Path: "/", MaxAge: 600})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `
		<body style="font-family:sans-serif; background:#0f172a; display:flex; justify-content:center; align-items:center; height:100vh; color:white; margin:0;">
			<form action="/verify-otp" method="POST" style="background:#1e293b; padding:40px; border-radius:24px; text-align:center;">
				<h2>Подтверждение</h2>
				<input name="otp" type="text" placeholder="000000" maxlength="6" required autofocus style="width:100%%; padding:15px; font-size:32px; text-align:center; border-radius:12px; margin-bottom:20px; border:none;">
				<button type="submit" style="width:100%%; background:#2563eb; color:white; border:none; padding:15px; border-radius:12px; font-weight:bold; cursor:pointer;">ВОЙТИ</button>
			</form>
		</body>`)
}

func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	input := strings.TrimSpace(r.FormValue("otp"))
	pending, err := r.Cookie("pending_user")
	var dbOtp, name, email string

	query := "SELECT TRIM(totp_secret), patient_name, user_email FROM appointments "
	if err == nil {
		query += fmt.Sprintf("WHERE user_email = '%s'", pending.Value)
	} else {
		query += "ORDER BY id DESC LIMIT 1"
	}

	db.QueryRow(query).Scan(&dbOtp, &name, &email)

	if input == dbOtp && dbOtp != "" {
		role := "patient"
		if email == DOCTOR_EMAIL {
			role = "doctor"
		}
		setCookie(w, "user_email", email)
		setCookie(w, "user_role", role)
		setCookie(w, "user_name", name)
		http.Redirect(w, r, "/", 302)
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<script>alert('Неверный код!'); history.back();</script>"))
	}
}

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
	fmt.Fprintf(w, `
	<!DOCTYPE html>
	<html>
	<head><meta charset="UTF-8"><title>HealthOS Dashboard</title><style>
		body { font-family: sans-serif; background: #f8fafc; margin: 0; display: flex; height: 100vh; }
		.sidebar { width: 260px; background: #0f172a; color: white; padding: 25px; }
		.content { flex: 1; padding: 40px; overflow-y: auto; }
		.card { background: white; padding: 25px; border-radius: 16px; box-shadow: 0 4px 6px -1px rgba(0,0,0,0.1); }
		table { width: 100%%; border-collapse: collapse; margin-top: 20px; }
		th, td { text-align: left; padding: 12px; border-bottom: 1px solid #edf2f7; }
	</style></head>
	<body>
		<div class="sidebar">
			<h2>HealthOS</h2>
			<p style="font-size: 14px; opacity: 0.8;">%s</p>
			<button onclick="location.href='/logout'" style="background:#ef4444; color:white; border:none; padding:10px; border-radius:8px; cursor:pointer; width:100%%; margin-top:20px;">Выйти</button>
		</div>
		<div class="content">
			<div id="doc-ui" class="card" style="display:none; margin-bottom:20px;">
				<h3>🩺 Панель врача</h3>
				<input id="pe" placeholder="Email пациента" style="padding:10px; margin-right:10px;">
				<input id="pd" placeholder="Диагноз" style="padding:10px; margin-right:10px;">
				<button onclick="save()" style="padding:10px 20px; background:#2563eb; color:white; border:none; border-radius:8px; cursor:pointer;">Сохранить</button>
			</div>
			<div class="card">
				<h3>История обследований</h3>
				<table id="table">
					<thead><tr><th>Дата</th><th>Имя</th><th>Диагноз</th></tr></thead>
					<tbody id="tbody"></tbody>
				</table>
			</div>
		</div>
		<script>
			const role = document.cookie.match('user_role=([^;]+)')?.[1];
			if(role === 'doctor') document.getElementById('doc-ui').style.display = 'block';

			function load() {
				fetch('/api/data').then(r => r.json()).then(data => {
					document.getElementById('tbody').innerHTML = data.map(i => 
						'<tr><td>'+i.date.split('T')[0]+'</td><td>'+i.name+'</td><td>'+(i.diag || 'В обработке...')+'</td></tr>'
					).join('');
				});
			}
			function save() {
				const email = document.getElementById('pe').value;
				const diagnosis = document.getElementById('pd').value;
				fetch('/api/data', {method:'POST', body: JSON.stringify({email, diagnosis})}).then(() => {
					document.getElementById('pd').value = '';
					load();
				});
			}
			load();
		</script>
	</body></html>`, cEmail.Value)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	for _, k := range []string{"user_email", "user_role", "user_name"} {
		http.SetCookie(w, &http.Cookie{Name: k, Value: "", Path: "/", MaxAge: -1})
	}
	http.Redirect(w, r, "/api/auth/google", 302)
}

// --- ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ---
func sendOTPEmail(toEmail, code string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	msg := []byte("Subject: HealthOS Code\n\nТвой код авторизации: " + code)
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, msg)
}

func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: 604800})
}
