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

	// Маршрут для смены аккаунта
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
	log.Printf("v2.3.0 LIVE - Multi-Account System & Premium UI")
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
    <title>HealthOS Premium</title>
    <link href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css" rel="stylesheet">
    <style>
        :root { --primary: #3b82f6; --dark: #0f172a; --bg: #f8fafc; }
        body { font-family: 'Inter', sans-serif; margin: 0; background: var(--bg); display: flex; height: 100vh; overflow: hidden; }
        
        .sidebar { width: 320px; background: var(--dark); color: white; display: flex; flex-direction: column; padding: 25px; transition: 0.3s; }
        .logo { font-size: 24px; font-weight: 900; background: linear-gradient(to right, #60a5fa, #2563eb); -webkit-background-clip: text; -webkit-text-fill-color: transparent; margin-bottom: 35px; }
        
        /* Личный кабинет */
        .profile-area { background: #1e293b; padding: 20px; border-radius: 20px; border: 1px solid #334155; margin-bottom: 30px; }
        .p-role { font-size: 10px; text-transform: uppercase; letter-spacing: 1.5px; color: #60a5fa; margin-bottom: 5px; font-weight: bold; }
        .p-email { font-size: 13px; font-weight: 600; color: #f1f5f9; word-break: break-all; }
        .p-otp { background: #0f172a; border-radius: 12px; padding: 12px; margin: 15px 0; text-align: center; font-size: 22px; font-family: monospace; color: #fff; border: 1px dashed #3b82f6; }
        
        .nav-link { padding: 14px 18px; border-radius: 12px; color: #94a3b8; text-decoration: none; display: flex; align-items: center; gap: 12px; margin-bottom: 8px; cursor: pointer; transition: 0.2s; }
        .nav-link:hover, .nav-link.active { background: var(--primary); color: white; box-shadow: 0 4px 12px rgba(59, 130, 246, 0.3); }

        .btn-logout { background: transparent; border: 1px solid #ef4444; color: #ef4444; padding: 12px; border-radius: 12px; width: 100%; cursor: pointer; font-weight: bold; margin-top: 10px; transition: 0.2s; }
        .btn-logout:hover { background: #ef4444; color: white; }

        /* Контент */
        .content { flex: 1; padding: 40px; overflow-y: auto; }
        .tab { display: none; animation: fadeInUp 0.4s ease; }
        .tab.active { display: block; }
        @keyframes fadeInUp { from { opacity: 0; transform: translateY(15px); } to { opacity: 1; transform: translateY(0); } }

        .card { background: white; border-radius: 24px; padding: 30px; box-shadow: 0 4px 6px -1px rgba(0,0,0,0.05); margin-bottom: 25px; border: 1px solid #f1f5f9; }
        .card h3 { margin: 0 0 20px 0; font-size: 18px; display: flex; align-items: center; gap: 10px; color: #1e293b; }

        table { width: 100%; border-collapse: collapse; }
        th { text-align: left; padding: 12px; color: #94a3b8; font-size: 12px; text-transform: uppercase; }
        td { padding: 16px 12px; border-top: 1px solid #f1f5f9; font-size: 14px; }

        .input-ui { background: #f8fafc; border: 1px solid #e2e8f0; padding: 12px 16px; border-radius: 12px; outline: none; transition: 0.2s; }
        .input-ui:focus { border-color: var(--primary); box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1); }
        .btn-primary { background: var(--primary); color: white; border: none; padding: 12px 24px; border-radius: 12px; font-weight: bold; cursor: pointer; }
    </style>
</head>
<body>
    <div class="sidebar">
        <div class="logo"><i class="fas fa-bolt"></i> HealthTech OS</div>
        
        <div id="profile-section">
            </div>

        <nav>
            <div class="nav-link active" onclick="switchTab('main')"><i class="fas fa-compass"></i> Обзор</div>
            <div class="nav-link" onclick="switchTab('doctors')"><i class="fas fa-user-md"></i> Мои врачи</div>
            <div class="nav-link" onclick="switchTab('bills')"><i class="fas fa-file-invoice-dollar"></i> Счета</div>
        </nav>
    </div>

    <div class="content">
        <div id="tab-main" class="tab active">
            <div id="doctor-controls" class="card" style="display:none; background: #f0f7ff;">
                <h3><i class="fas fa-shield-halved"></i> Консоль доктора</h3>
                <div style="display:grid; grid-template-columns: 1fr 2fr auto; gap: 15px;">
                    <input id="p-email" class="input-ui" placeholder="Email пациента">
                    <input id="p-diag" class="input-ui" placeholder="Медицинское заключение">
                    <button class="btn-primary" onclick="saveData()">Обновить</button>
                </div>
            </div>

            <div class="card">
                <h3><i class="fas fa-notes-medical"></i> Журнал пациентов</h3>
                <table>
                    <thead><tr><th>Дата</th><th>Пациент</th><th>Заключение</th></tr></thead>
                    <tbody id="data-table"></tbody>
                </table>
            </div>

            <div class="card">
                <h3><i class="fas fa-comments"></i> Сообщения</h3>
                <div id="chat-window" style="height:250px; overflow-y:auto; margin-bottom:20px; padding:15px; background:#f8fafc; border-radius:15px; line-height:1.6;"></div>
                <div style="display:flex; gap:12px;">
                    <input id="chat-in" class="input-ui" style="flex:1" placeholder="Ваше сообщение...">
                    <button class="btn-primary" onclick="sendMsg()"><i class="fas fa-paper-plane"></i></button>
                </div>
            </div>
        </div>

        <div id="tab-doctors" class="tab">
            <div class="card">
                <h3>Специалисты</h3>
                <div style="display:flex; gap:20px; align-items:center; background:#f8fafc; padding:20px; border-radius:15px;">
                    <i class="fas fa-user-circle fa-4x" style="color:#cbd5e1"></i>
                    <div>
                        <div style="font-weight:800; font-size:18px;">Махамбет Нур</div>
                        <div style="color:#64748b;">Главный врач информационных систем</div>
                    </div>
                </div>
            </div>
        </div>

        <div id="tab-bills" class="tab">
            <div class="card">
                <h3>Баланс и ОСМС</h3>
                <div style="background:#dcfce7; color:#166534; padding:25px; border-radius:20px; font-weight:bold; display:flex; align-items:center; gap:15px;">
                    <i class="fas fa-check-circle fa-2x"></i>
                    Ваша страховка активна. Все услуги бесплатны.
                </div>
            </div>
        </div>
    </div>

    <script>
        function switchTab(name) {
            document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.nav-link').forEach(l => l.classList.remove('active'));
            document.getElementById('tab-'+name).classList.add('active');
            event.currentTarget.classList.add('active');
        }

        function getCookie(name) {
            let matches = document.cookie.match(new RegExp("(?:^|; )" + name.replace(/([\.$?*|{}\(\)\[\]\\\/\+^])/g, '\\$1') + "=([^;]*)"));
            return matches ? decodeURIComponent(matches[1]) : undefined;
        }

        const email = getCookie('user_email');
        const role = getCookie('user_role');
        const otp = getCookie('user_otp');

		const profileSection = document.getElementById('profile-section');
		if(email) {
			profileSection.innerHTML = "<div class=\"profile-area\"><div class=\"p-role\">" + (role === 'doctor' ? 'Главный врач' : 'Кабинет пациента') + "</div><div class=\"p-email\">" + email + "</div><div class=\"p-otp\">" + (otp || '000000') + "</div><button class=\"btn-logout\" onclick=\"location.href='/logout'\"><i class=\"fas fa-sync-alt\"></i> Сменить аккаунт</button></div>";
			if(role === 'doctor') document.getElementById('doctor-controls').style.display = 'block';
		} else {
			profileSection.innerHTML = '<button class="btn-primary" style="width:100%; margin-bottom:30px;" onclick="location.href=\'/api/auth/google\'">Войти через Google</button>';
		}

        function refresh() {
            fetch('/api/data').then(r => r.json()).then(data => {
                document.getElementById('data-table').innerHTML = data.map(i => 
                    '<tr><td>'+i.date.split('T')[0]+'</td><td>'+i.email+'</td><td><span style="background:#f1f5f9; padding:6px 12px; border-radius:8px; font-weight:bold;">'+(i.diag || 'В очереди')+'</span></td></tr>'
                ).join('');
            });
            fetch('/api/chat').then(r => r.json()).then(msgs => {
                document.getElementById('chat-window').innerHTML = msgs.map(m => '<div style="margin-bottom:8px;"><b>'+m.sender.split('@')[0]+':</b> '+m.text+'</div>').join('');
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
