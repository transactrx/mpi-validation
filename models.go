package mpivalidation

import "time"

// InboundPatientIdRequest is the request sent to the MPI service for patient lookup/creation.
type InboundPatientIdRequest struct {
	Id        string `json:"id"`
	LastName  string `json:"lastName"`
	FirstName string `json:"firstName"`
	Gender    string `json:"gender"` // 1 - Male, 2 - Female, 3 - Other, 0 - Unknown
	DOB       string `json:"dob"`
	Street    string `json:"street"`
	City      string `json:"city"`
	State     string `json:"state"`
	Zip       string `json:"zip"`
	Phone     string `json:"phone"`

	Meds          []string `json:"meds"`
	PrescriberNpi string   `json:"prescriberNpi"`

	PharmacyNpi          string `json:"pharmacyNpi"`
	PharmacyNCPDP        string `json:"pharmacyNCPDP"`
	RxPatientId          string `json:"rxPatientId"`
	RxPatientIdQualifier string `json:"rxPatientIdQualifier"`

	Bin                   string    `json:"bin"`
	CardHolderId          string    `json:"cardHolderId"`
	GroupNumber           string    `json:"groupNumber"`
	RelationshipCode      string    `json:"relationshipCode"`
	PCN                   string    `json:"pcn"`
	CardholderFirst       string    `json:"cardholderFirst"`
	CardholderLast        string    `json:"cardholderLast"`
	EventDate             time.Time `json:"eventDate"`
	HasMultipleInsurances *bool     `json:"hasMultipleInsurances,omitempty"`
}

// MasterPatIndexResponse is the MPI service response containing the resolved patient ID.
type MasterPatIndexResponse struct {
	PatientID    string `json:"patientId"`
	SearchStatus string `json:"searchStatus"`
}

// PatientClaimRecordIdLinkEvent links a patient ID to a claim record ID after MPI lookup.
type PatientClaimRecordIdLinkEvent struct {
	PatientId     string `json:"patientId"`
	ClaimRecordId string `json:"claimRecordId"`
	SearchStatus  string `json:"searchStatus"`
}

// InvalidateInsuranceRequest is the request to invalidate a specific insurance entry.
type InvalidateInsuranceRequest struct {
	FirstName     string     `json:"firstName"`
	LastName      string     `json:"lastName"`
	Gender        string     `json:"gender"`
	DOB           string     `json:"dob"`
	Bin           string     `json:"bin"`
	PCN           string     `json:"pcn"`
	CardHolderId  string     `json:"cardHolderId"`
	GroupNumber   string     `json:"groupNumber"`
	InvalidatedAt *time.Time `json:"invalidatedAt,omitempty"`
}

// InvalidateInsuranceResponse is the MPI service response for insurance invalidation.
type InvalidateInsuranceResponse struct {
	Found     bool   `json:"found"`
	PatientID string `json:"patientId,omitempty"`
}

// InsuranceInvalidatedEvent is published after an insurance invalidation completes.
type InsuranceInvalidatedEvent struct {
	PatientId     string    `json:"patientId"`
	RecordId      string    `json:"recordId"`
	Bin           string    `json:"bin"`
	PCN           string    `json:"pcn"`
	CardHolderId  string    `json:"cardHolderId"`
	GroupNumber   string    `json:"groupNumber"`
	Found         bool      `json:"found"`
	InvalidatedAt time.Time `json:"invalidatedAt"`
}
