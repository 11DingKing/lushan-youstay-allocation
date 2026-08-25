package domain

import "time"

type ResourceKind string

const (
	ResourceVillaRoom    ResourceKind = "villa_room"
	ResourceMountainRoom ResourceKind = "mountain_room"
	ResourceRVBert       ResourceKind = "rv_berth"
	ResourceCampSite     ResourceKind = "camp_site"
)

type ResourceStatus string

const (
	ResourceOpen     ResourceStatus = "open"
	ResourceHeld     ResourceStatus = "held"
	ResourceOccupied ResourceStatus = "occupied"
	ResourceCleaning ResourceStatus = "cleaning"
	ResourceFaulted  ResourceStatus = "faulted"
	ResourceClosed   ResourceStatus = "closed"
)

type PropertyStatus string

const (
	PropertyOpen              PropertyStatus = "open"
	PropertyWeatherClosed     PropertyStatus = "weather_closed"
	PropertyMaintenanceClosed PropertyStatus = "maintenance_closed"
)

type Property struct {
	ID        string
	Name      string
	Zone      string
	Timezone  string
	Status    PropertyStatus
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (p Property) CanAcceptArrivals() bool { return p.Status == PropertyOpen }

func (p Property) Validate() error {
	if err := Require(p.ID != "", "id", "is required"); err != nil {
		return err
	}
	if err := Require(p.Name != "", "name", "is required"); err != nil {
		return err
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return &FieldError{Field: "timezone", Message: "is not recognized"}
	}
	switch p.Status {
	case PropertyOpen, PropertyWeatherClosed, PropertyMaintenanceClosed:
		return nil
	default:
		return &FieldError{Field: "status", Message: "is not recognized"}
	}
}

type Resource struct {
	ID             string
	PropertyID     string
	Code           string
	Name           string
	Kind           ResourceKind
	Capacity       int
	BasePriceCents int64
	Status         ResourceStatus
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (r Resource) Validate() error {
	if err := Require(r.ID != "", "id", "is required"); err != nil {
		return err
	}
	if err := Require(r.PropertyID != "", "property_id", "is required"); err != nil {
		return err
	}
	if err := Require(r.Code != "", "code", "is required"); err != nil {
		return err
	}
	if err := Require(r.Capacity > 0, "capacity", "must be positive"); err != nil {
		return err
	}
	if err := Require(r.BasePriceCents >= 0, "base_price_cents", "cannot be negative"); err != nil {
		return err
	}
	switch r.Kind {
	case ResourceVillaRoom, ResourceMountainRoom, ResourceRVBert, ResourceCampSite:
	default:
		return &FieldError{Field: "kind", Message: "is not recognized"}
	}
	switch r.Status {
	case ResourceOpen, ResourceHeld, ResourceOccupied, ResourceCleaning, ResourceFaulted, ResourceClosed:
		return nil
	default:
		return &FieldError{Field: "status", Message: "is not recognized"}
	}
}

func (r Resource) CanHost(partySize int) bool {
	return partySize > 0 && partySize <= r.Capacity && r.Status != ResourceFaulted && r.Status != ResourceClosed
}

func (r Resource) Transition(to ResourceStatus) (Resource, error) {
	allowed := map[ResourceStatus]map[ResourceStatus]bool{
		ResourceOpen:     {ResourceHeld: true, ResourceOccupied: true, ResourceFaulted: true, ResourceClosed: true},
		ResourceHeld:     {ResourceOpen: true, ResourceOccupied: true, ResourceFaulted: true, ResourceClosed: true},
		ResourceOccupied: {ResourceCleaning: true, ResourceFaulted: true},
		ResourceCleaning: {ResourceOpen: true, ResourceFaulted: true},
		ResourceFaulted:  {ResourceCleaning: true, ResourceClosed: true, ResourceOpen: true},
		ResourceClosed:   {ResourceOpen: true},
	}
	if r.Status == to {
		return r, nil
	}
	if !allowed[r.Status][to] {
		return Resource{}, &StateError{Entity: "resource", From: string(r.Status), To: string(to)}
	}
	r.Status = to
	r.Version++
	return r, nil
}
