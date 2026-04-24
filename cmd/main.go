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

// Константа для почты врача (замени на свою, если нужно)
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
	// Подключение к базе данных Render PostgreSQL
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	rand.Seed(time.Now().UnixNano())

	// Роуты API
	http.HandleFunc("/api/auth/google", handleLogin)
	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/api/data", handleData)
	http.HandleFunc("/api/chat", handleChat)
	http.HandleFunc("/logout", handleLogout)
	http.HandleFunc("/", handleRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("HealthTech OS v2.5.3 | Port: %s | Status: Online", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// 1. ВХОД ЧЕРЕЗ GOOGLE
func handleLogin(w http.ResponseWriter, r *http.Request) {
	// prompt=select_account заставляет Google всегда спрашивать выбор почты
	url := googleOAuthConfig.AuthCodeURL("state", oauth2.SetAuthURLParam("prompt", "select_account"))
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// 2. CALLBACK (ОБРАБОТКА ПОСЛЕ ВХОДА)
func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	token, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	client := googleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
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
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	// Определение роли
	role := "patient"
	if user.Email == DOCTOR_EMAIL {
		role = "doctor"
	}
	otp := fmt.Sprintf("%06d", rand.Intn(1000000))

	// Сохранение в БД (PostgreSQL)
	_, _ = db.Exec(`INSERT INTO appointments (user_email, role, patient_name, totp_secret, appointment_date) 
		VALUES ($1, $2, $3, $4, NOW()) 
		ON CONFLICT (user_email) DO UPDATE SET patient_name = $3, role = $2`,
		user.Email, role, user.Name, otp)

	// Установка куки
	setCookie(w, "user_email", user.Email)
	setCookie(w, "user_role", role)
	setCookie(w, "user_otp", otp)
	setCookie(w, "user_name", user.Name)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// 3. ПОЛУЧЕНИЕ И ОБНОВЛЕНИЕ ДАННЫХ (GET/POST)
func handleData(w http.ResponseWriter, r *http.Request) {
	cEmail, _ := r.Cookie("user_email")
	cRole, _ := r.Cookie("user_role")

	if r.Method == "POST" && cRole != nil && cRole.Value == "doctor" {
		var req struct {
			Email     string `json:"email"`
			Diagnosis string `json:"diagnosis"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			db.Exec("UPDATE appointments SET diagnosis = $1 WHERE user_email = $2", req.Diagnosis, req.Email)
		}
		return
	}

	// SQL Запрос с фильтром по роли
	query := "SELECT id, user_email, diagnosis, appointment_date, patient_name FROM appointments"
	if cRole != nil && cRole.Value == "patient" {
		query += " WHERE user_email = '" + cEmail.Value + "'"
	}

	rows, err := db.Query(query + " ORDER BY id DESC")
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id int
		var email, diag, date, name string
		rows.Scan(&id, &email, &diag, &date, &name)
		list = append(list, map[string]interface{}{
			"id":    id,
			"email": email,
			"diag":  diag,
			"date":  date,
			"name":  name,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// 4. ЧАТ
func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var msg struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		cEmail, _ := r.Cookie("user_email")
		if cEmail != nil {
			db.Exec("INSERT INTO messages (sender, text) VALUES ($1, $2)", cEmail.Value, msg.Text)
		}
	} else {
		rows, err := db.Query("SELECT sender, text FROM messages ORDER BY id ASC")
		if err != nil {
			return
		}
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msgs)
	}
}

// 5. ВЫХОД
func handleLogout(w http.ResponseWriter, r *http.Request) {
	names := []string{"user_email", "user_role", "user_otp", "user_name"}
	for _, name := range names {
		http.SetCookie(w, &http.Cookie{
			Name:   name,
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// 6. ГЛАВНАЯ СТРАНИЦА (HTML)
func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <title>HealthOS | Личный кабинет</title>
    <link href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css" rel="stylesheet">
    <style>
        :root { --primary: #2563eb; --dark: #0f172a; --bg: #f8fafc; }
        body { font-family: 'Inter', sans-serif; margin: 0; background: var(--bg); display: flex; height: 100vh; color: #334155; }
        
        .sidebar { width: 300px; background: #0f172a; color: white; display: flex; flex-direction: column; padding: 30px 20px; }
        .logo { font-size: 24px; font-weight: 800; color: #38bdf8; margin-bottom: 40px; display: flex; align-items: center; gap: 10px; }
        
        .user-panel { background: #1e293b; padding: 20px; border-radius: 16px; margin-bottom: 25px; border: 1px solid #334155; }
        .u-role { font-size: 10px; text-transform: uppercase; color: #38bdf8; font-weight: bold; }
        .u-name { font-size: 16px; font-weight: 600; margin: 5px 0; color: #f1f5f9; }
        .u-otp { font-family: monospace; font-size: 22px; color: #fff; background: #0f172a; padding: 10px; border-radius: 10px; text-align: center; margin: 15px 0; border: 1px dashed #2563eb; }

        .nav-link { padding: 12px 15px; border-radius: 10px; color: #94a3b8; cursor: pointer; display: flex; align-items: center; gap: 12px; margin-bottom: 8px; transition: 0.2s; }
        .nav-link:hover, .nav-link.active { background: var(--primary); color: white; }

        .exit-btn { background: #ef4444; color: white; border: none; padding: 12px; border-radius: 10px; cursor: pointer; width: 100%; font-weight: bold; margin-top: auto; }

        .main { flex: 1; padding: 40px; overflow-y: auto; }
        .card { background: white; border-radius: 20px; padding: 25px; box-shadow: 0 4px 6px -1px rgba(0,0,0,0.05); margin-bottom: 25px; border: 1px solid #e2e8f0; }
        
        .status-card { background: linear-gradient(135deg, #2563eb, #1d4ed8); color: white; padding: 25px; border-radius: 20px; display: flex; justify-content: space-between; align-items: center; }

        table { width: 100%; border-collapse: collapse; }
        td { padding: 15px 12px; border-top: 1px solid #f1f5f9; font-size: 14px; }
        .badge { background: #f0fdf4; color: #166534; padding: 6px 12px; border-radius: 20px; font-weight: 600; font-size: 12px; }

        .input { width: 100%; padding: 12px; border: 1px solid #e2e8f0; border-radius: 10px; margin-bottom: 10px; outline: none; }
    </style>
</head>
<body>
    <div class="sidebar">
        <div class="logo"><i class="fas fa-stethoscope"></i> HealthOS</div>
        <div id="side-profile"></div>
        <nav>
            <div class="nav-link active"><i class="fas fa-house"></i> Главная</div>
            <div class="nav-link"><i class="fas fa-file-medical"></i> Медкарта</div>
        </nav>
        <button class="exit-btn" onclick="location.href='/logout'">Выход</button>
    </div>

    <div class="main">
        <div id="doc-tool" class="card" style="display:none; border-top: 5px solid #f59e0b;">
            <h3>Панель управления врача</h3>
            <div style="display:grid; grid-template-columns: 1fr 2fr auto; gap:10px;">
                <input id="t-mail" class="input" placeholder="Почта пациента">
                <input id="t-diag" class="input" placeholder="Диагноз">
                <button onclick="save()" style="background:var(--primary); color:white; border:none; padding:10px 20px; border-radius:10px; cursor:pointer;">ОК</button>
            </div>
        </div>

        <div class="status-card">
            <div>
                <div style="font-size: 12px; opacity: 0.8;">СТАТУС ОСМС</div>
                <div style="font-size: 24px; font-weight: 800;">ЗАСТРАХОВАН</div>
                <div style="margin-top:10px; font-size: 13px;"><i class="fas fa-location-dot"></i> Атырау, Поликлиника №1</div>
            </div>
            <i class="fas fa-shield-check fa-4x" style="opacity: 0.2;"></i>
        </div>

        <div class="card" style="margin-top:25px;">
            <h3><i class="fas fa-history"></i> Журнал записей</h3>
            <table><tbody id="data-body"></tbody></table>
        </div>
    </div>

    <script>
        const getC = (n) => document.cookie.match('(^|;) ?'+n+'=([^;]*)(;|$)')?. [2];
        const email = getC('user_email');
        const role = getC('user_role');
        const name = getC('user_name');
        const otp = getC('user_otp');

        if(email) {
            document.getElementById('side-profile').innerHTML = 
                '<div class="user-panel"><div class="u-role">'+(role==='doctor'?'ВРАЧ':'ПАЦИЕНТ')+'</div><div class="u-name">'+decodeURIComponent(name)+'</div><div style="font-size:11px; color:#64748b;">'+email+'</div><div class="u-otp">'+otp+'</div></div>';
            if(role === 'doctor') document.getElementById('doc-tool').style.display = 'block';
        } else {
            location.href = '/api/auth/google';
        }

        function refresh() {
            fetch('/api/data').then(r => r.json()).then(data => {
                document.getElementById('data-body').innerHTML = data.map(d => 
                    '<tr><td>'+d.date.split('T')[0]+'</td><td><b>'+d.name+'</b></td><td><span class="badge">'+(d.diag || 'Ожидает')+'</span></td></tr>'
                ).join('');
            });
        }

        function save() {
            const email = document.getElementById('t-mail').value;
            const diagnosis = document.getElementById('t-diag').value;
            fetch('/api/data', { method: 'POST', body: JSON.stringify({email, diagnosis}) }).then(() => {
                alert('Обновлено'); refresh();
            });
        }

        setInterval(refresh, 5000);
        refresh();
    </script>
</body>
</html>
	`)
}

// Вспомогательная функция для куки
func setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:   name,
		Value:  value,
		Path:   "/",
		MaxAge: 604800,
	})
}
