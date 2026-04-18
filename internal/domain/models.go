package domain

import "time"

// Appointment описывает одну запись в системе
type Appointment struct {
	ID              int       `json:"id"`
	PatientName     string    `json:"patient_name"`
	AppointmentDate time.Time `json:"appointment_date"`
	Status          string    `json:"status"`
}
