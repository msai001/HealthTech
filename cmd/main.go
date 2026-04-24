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
	log.Printf("Starting v2.1.0 - Fixed Syntax & Ultra Design")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func cookieClear(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:   name,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
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

	// Исправление: проверка ошибки перед использованием resp
	if err != nil || resp == nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	defer resp.Body.Close()

	var user struct {
		Email string `json:"email"`
		Name  string `json:"name"`
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
	http.SetCookie(w, &http.Cookie{
		Name:   name,
		Value:  value,
		Path:   "/",
		MaxAge: 604800,
	})
}

func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, _ := r.Cookie("user_email")
	cRole, _ := r.Cookie("user_role")

	if r.Method == "POST" && cRole != nil && cRole.Value == "doctor" {
		var req struct {
			Email     string `json:"email"`
			Diagnosis string `json:"diagnosis"`
		}
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
			list = append(list, map[string]interface{}{
				"id":    id,
				"email": email,
				"diag":  diag,
				"date":  date,
				"otp":   otp,
			})
		}
		json.NewEncoder(w).Encode(list)
	}
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var msg struct {
			Text string `json:"text"`
		}
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
				msgs = append(msgs, map[string]string{
					"sender": s,
					"text":   t,
				})
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
    <title>HealthOS | Premium Portal</title>
    <link href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css" rel="stylesheet">
    <style>
        :root { 
            --primary: #0062ff; 
            --bg: #f0f2f5; 
            --sidebar: #111827; 
            --glass: rgba(255, 255, 255, 0.9);
        }
        body { font-family: 'Inter', sans-serif; margin: 0; background: var(--bg); display: flex; height: 100vh; color: #1f2937; }
        
        /* Sidebar */
        .sidebar { width: 280px; background: var(--sidebar); color: white; display: flex; flex-direction: column; padding: 25px; box-shadow: 4px 0 15px rgba(0,0,0,0.1); }
        .logo { font-size: 24px; font-weight: 800; color: #3b82f6; margin-bottom: 40px; display: flex; align-items: center; gap: 10px; }
        
        .profile-card { background: #1f2937; padding: 20px; border-radius: 16px; margin-bottom: 25px; border: 1px solid #374151; }
        .otp-display { background: #111827; color: #60a5fa; padding: 12px; border-radius: 10px; text-align: center; font-family: monospace; font-size: 20px; margin: 15px 0; border: 1px dashed #3b82f6; }
        
        .nav-item { padding: 14px 18px; border-radius: 12px; color: #9ca3af; cursor: pointer; display: flex; align-items: center; gap: 12px; transition: 0.3s; margin-bottom: 5px; }
        .nav-item:hover, .nav-item.active { background: #2563eb; color: white; box-shadow: 0 4px 12px rgba(37, 99, 235, 0.3); }

        /* Main */
        .main { flex: 1; padding: 40px; overflow-y: auto; }
        .tab-content { display: none; animation: slideUp 0.4s ease; }
        .tab-content.active { display: block; }
        @keyframes slideUp { from { opacity: 0; transform: translateY(20px); } to { opacity: 1; transform: translateY(0); } }

        .card { background: var(--glass); backdrop-filter: blur(10px); border-radius: 24px; padding: 32px; box-shadow: 0 10px 25px rgba(0,0,0,0.03); margin-bottom: 24px; border: 1px solid white; }
        .card h2 { margin-top: 0; font-size: 20px; display: flex; align-items: center; gap: 10px; }

        table { width: 100%; border-collapse: collapse; }
        th { text-align: left; padding: 12px; color: #6b7280; font-size: 13px; text-transform: uppercase; }
        td { padding: 16px 12px; border-top: 1px solid #e5e7eb; }

        .btn { padding: 12px 24px; border-radius: 12px; border: none; cursor: pointer; font-weight: 600; transition: 0.2s; display: flex; align-items: center; gap: 8px; }
        .btn-primary { background: #2563eb; color: white; }
        .btn-primary:hover { background: #1d4ed8; transform: translateY(-2px); }
        .btn-danger { background: #ef4444; color: white; width: 100%; justify-content: center; }

        .input-pill { background: #f9fafb; border: 1px solid #e5e7eb; padding: 12px 16px; border-radius: 12px; outline: none; width: 100%; }
        .input-pill:focus { border-color: #3b82f6; box-shadow: 0 0 0 4px rgba(59, 130, 246, 0.1); }
    </style>
</head>
<body>
    <div class="sidebar">
        <div class="logo"><i class="fas fa-heart-pulse"></i> HealthOS</div>
        
        <div id="auth-section">
            </div>

        <nav style="margin-top:20px;">
            <div class="nav-item active" onclick="showTab('main')"><i class="fas fa-chart-pie"></i> Обзор</div>
            <div class="nav-item" onclick="showTab('doctors')"><i class="fas fa-stethoscope"></i> Врачи</div>
            <div class="nav-item" onclick="showTab('billing')"><i class="fas fa-wallet"></i> Счета</div>
        </nav>
    </div>

    <div class="main">
        <div id="tab-main" class="tab-content active">
            <div id="doctor-panel" class="card" style="display:none; background: #eff6ff; border-color: #bfdbfe;">
                <h2><i class="fas fa-user-shield"></i> Режим врача</h2>
                <div style="display:grid; grid-template-columns: 1fr 2fr auto; gap: 15px; margin-top: 15px;">
                    <input id="p-email" class="input-pill" placeholder="Email пациента">
                    <input id="p-diag" class="input-pill" placeholder="Диагноз / Предписание">
                    <button class="btn btn-primary" onclick="saveData()">Обновить</button>
                </div>
            </div>

            <div class="card">
                <h2><i class="fas fa-list-check"></i> История обращений</h2>
                <table>
                    <thead><tr><th>Дата</th><th>Пациент</th><th>Заключение</th></tr></thead>
                    <tbody id="data-table"></tbody>
                </table>
            </div>

            <div class="card">
                <h2><i class="fas fa-comment-dots"></i> Онлайн консультация</h2>
                <div id="chat-box" style="height:250px; overflow-y:auto; margin-bottom:20px; padding:10px; background:#f9fafb; border-radius:15px;"></div>
                <div style="display:flex; gap:10px;">
                    <input id="chat-in" class="input-pill" placeholder="Ваш вопрос...">
                    <button class="btn btn-primary" onclick="sendMsg()"><i class="fas fa-paper-plane"></i></button>
                </div>
            </div>
        </div>

        <div id="tab-doctors" class="tab-content">
            <div class="card">
                <h2>Мои врачи</h2>
                <div style="display:grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 20px;">
                    <div style="text-align:center; padding:20px; border:1px solid #eee; border-radius:15px;">
                        <i class="fas fa-user-circle fa-3x" style="color:#ddd"></i>
                        <p><b>Махамбет Нур</b><br><small>Главврач</small></p>
                    </div>
                </div>
            </div>
        </div>

        <div id="tab-billing" class="tab-content">
            <div class="card">
                <h2>Финансовый кабинет (ОСМС)</h2>
                <p>Все ваши визиты покрыты обязательным страхованием.</p>
                <div style="padding:15px; background:#f0fdf4; color:#166534; border-radius:12px;">Статус: Задолженностей нет</div>
            </div>
        </div>
    </div>

    <script>
        function showTab(name) {
            document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
            document.getElementById('tab-'+name).classList.add('active');
            event.currentTarget.classList.add('active');
        }

        function getC(n) {
            let b = document.cookie.match('(^|;)\\s*' + n + '\\s*=\\s*([^;]+)');
            return b ? decodeURIComponent(b.pop()) : "";
        }

        const email = getC('user_email');
        const role = getC('user_role');
        const otp = getC('user_otp');

        const authBox = document.getElementById('auth-section');
		if(email) {
			authBox.innerHTML = ""
				+ "<div class=\"profile-card\">"
				+   "<div style=\"font-size:12px; color:#9ca3af;\">" + (role === 'doctor' ? "👨‍⚕️ Администратор" : "👤 Пациент") + "</div>"
				+   "<div style=\"font-weight:700; margin:5px 0; overflow:hidden; text-overflow:ellipsis;\">" + email + "</div>"
				+   "<div class=\"otp-display\">" + (otp || "------") + "</div>"
				+   "<a href=\"/logout\" class=\"btn btn-danger\" style=\"text-decoration:none;\">Выйти</a>"
				+ "</div>";
			if(role === 'doctor') document.getElementById('doctor-panel').style.display = 'block';
		} else {
            authBox.innerHTML = '<a href="/api/auth/google" class="btn btn-primary" style="text-decoration:none; width:100%; justify-content:center;"><i class="fab fa-google"></i> Войти</a>';
        }

        function refresh() {
            fetch('/api/data').then(r => r.json()).then(data => {
                document.getElementById('data-table').innerHTML = data.map(i => 
                    '<tr><td>'+i.date.split('T')[0]+'</td><td>'+i.email+'</td><td><span style="padding:5px 10px; background:#eef2ff; border-radius:6px; font-weight:600;">'+(i.diag || 'Ожидает')+'</span></td></tr>'
                ).join('');
            });
            fetch('/api/chat').then(r => r.json()).then(msgs => {
                document.getElementById('chat-box').innerHTML = msgs.map(m => '<div style="margin-bottom:10px;"><b>'+m.sender.split('@')[0]+':</b> '+m.text+'</div>').join('');
            });
        }

        function saveData() {
            fetch('/api/data', {
                method: 'POST',
                body: JSON.stringify({email: document.getElementById('p-email').value, diagnosis: document.getElementById('p-diag').value})
            }).then(() => { alert('Сохранено!'); refresh(); });
        }

        function sendMsg() {
            fetch('/api/chat', {
                method: 'POST',
                body: JSON.stringify({text: document.getElementById('chat-in').value})
            }).then(() => { document.getElementById('chat-in').value = ''; refresh(); });
        }

        if(email) { setInterval(refresh, 5000); refresh(); }
    </script>
</body>
</html>
	`)
}
