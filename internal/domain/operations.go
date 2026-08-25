package domain

import "time"

type TaskKind string
type TaskStatus string

const (
	TaskCleaning   TaskKind   = "cleaning"
	TaskInspection TaskKind   = "inspection"
	TaskRepair     TaskKind   = "repair"
	TaskRelocation TaskKind   = "relocation"
	TaskPending    TaskStatus = "pending"
	TaskClaimed    TaskStatus = "claimed"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
)

type OperationalTask struct {
	ID           string
	StayID       *string
	ResourceID   string
	Kind         TaskKind
	Status       TaskStatus
	AssignedRole string
	Attempts     int
	AvailableAt  time.Time
	LastError    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (t OperationalTask) Claim(now time.Time) (OperationalTask, error) {
	if t.Status != TaskPending || now.Before(t.AvailableAt) {
		return OperationalTask{}, &StateError{Entity: "operational_task", From: string(t.Status), To: string(TaskClaimed)}
	}
	t.Status = TaskClaimed
	t.Attempts++
	t.UpdatedAt = now.UTC()
	return t, nil
}

func (t OperationalTask) Complete(now time.Time) (OperationalTask, error) {
	if t.Status != TaskClaimed {
		return OperationalTask{}, &StateError{Entity: "operational_task", From: string(t.Status), To: string(TaskCompleted)}
	}
	t.Status = TaskCompleted
	t.LastError = nil
	t.UpdatedAt = now.UTC()
	return t, nil
}

func (t OperationalTask) Retry(now time.Time, cause error, maxAttempts int, backoff time.Duration) OperationalTask {
	message := cause.Error()
	t.LastError = &message
	t.UpdatedAt = now.UTC()
	if t.Attempts >= maxAttempts {
		t.Status = TaskFailed
		return t
	}
	t.Status = TaskPending
	t.AvailableAt = now.Add(backoff * time.Duration(t.Attempts))
	return t
}

type DamageStatus string

const (
	DamageDraft    DamageStatus = "draft"
	DamageAccepted DamageStatus = "accepted"
	DamageDisputed DamageStatus = "disputed"
	DamageWaived   DamageStatus = "waived"
)

type DamageClaim struct {
	ID          string
	StayID      string
	TaskID      string
	AssessedBy  string
	Description string
	AmountCents int64
	Status      DamageStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SettlementStatus string

const (
	SettlementPending    SettlementStatus = "pending"
	SettlementProcessing SettlementStatus = "processing"
	SettlementCompleted  SettlementStatus = "completed"
	SettlementFailed     SettlementStatus = "failed"
)

type RefundSettlement struct {
	ID          string
	StayID      string
	PaidCents   int64
	DamageCents int64
	RefundCents int64
	Status      SettlementStatus
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewSettlement(id, stayID string, paid, damage int64, now time.Time) (RefundSettlement, error) {
	if paid < 0 || damage < 0 {
		return RefundSettlement{}, &FieldError{Field: "amount", Message: "cannot be negative"}
	}
	refund := paid - damage
	if refund < 0 {
		refund = 0
	}
	return RefundSettlement{ID: id, StayID: stayID, PaidCents: paid, DamageCents: damage,
		RefundCents: refund, Status: SettlementPending, Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
}
