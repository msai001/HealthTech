package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
	Endpoint:     google.Endpoint,
}

func main() {
	// 1. Главная страница
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><body style="text-align:center;padding:50px;font-family:Arial;">
			<h1>HealthTech System</h1>
			<a href="%s" style="background:#4285F4;color:white;padding:15px;text-decoration:none;border-radius:5px;font-weight:bold;">Войти через Google</a>
		</body></html>`, url)
	})

	// 2. Callback (выдает форму)
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Код не получен", http.StatusBadRequest)
			return
		}
		_, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			http.Error(w, "Ошибка токена", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `
			<html><body style="font-family:Arial; background:#f4f7f6; display:flex; justify-content:center; padding:20px;">
				<div style="background:white; padding:30px; border-radius:10px; box-shadow:0 2px 10px rgba(0,0,0,0.1); width:400px;">
					<h2>Запись пациента</h2>
					<form action="/save" method="POST">
						<p>Имя пациента:<br><input type="text" name="name" style="width:100%%; padding:8px;" required></p>
						<p>Дата приема:<br><input type="date" name="date" style="width:100%%; padding:8px;" required></p>
						<p>Врач:<br><select name="doctor" style="width:100%%; padding:8px;">
							<option>Терапевт</option>
							<option>Хирург</option>
						</select></p>
						<button type="submit" style="width:100%%; background:#27ae60; color:white; border:none; padding:10px; border-radius:5px; cursor:pointer;">Записать</button>
					</form>
				</div>
			</body></html>`)
	})

	// 3. Сохранение (страница успеха)
	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		name := r.FormValue("name")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `
			<div style="text-align:center; padding:50px; font-family:Arial;">
				<h2 style="color:#27ae60;">✅ Пациент %s записан!</h2>
				<br><a href="/" style="background:#4285F4; color:white; padding:10px; text-decoration:none; border-radius:5px;">На главную</a>
			</div>`, name)
	})

	// 4. Запуск
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
