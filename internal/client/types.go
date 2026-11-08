package client

import "strings"

// The /v1 shapes this provider reads, from https://moq.pro/doc/api.json.
// Unknown response fields are ignored, so additive API changes are safe.

type Public struct {
	V         int      `json:"v"`
	Publish   []string `json:"publish,omitempty"`
	Subscribe []string `json:"subscribe,omitempty"`
}

// V1 returns the patterns in version 1 form. A legacy prefix `foo` is the
// subtree `foo/**` and an empty prefix is `**`, the API's own migration.
func (p *Public) V1() *Public {
	if p == nil || p.V == 1 {
		return p
	}
	migrate := func(prefixes []string) []string {
		var out []string
		for _, prefix := range prefixes {
			prefix = strings.TrimRight(prefix, "/")
			if prefix == "" {
				out = append(out, "**")
			} else {
				out = append(out, prefix+"/**")
			}
		}
		return out
	}
	return &Public{V: 1, Publish: migrate(p.Publish), Subscribe: migrate(p.Subscribe)}
}

type Project struct {
	ID         string  `json:"id"`
	Name       *string `json:"name"`
	Domain     *string `json:"domain"`
	DomainRole string  `json:"domainRole"`
	CreatedAt  string  `json:"createdAt"`
	Role       string  `json:"role"`
	Public     *Public `json:"public"`
}

type SpendCap struct {
	SpendCapMicros *int64 `json:"spendCapMicros"`
}

type KeyScope struct {
	Root      string   `json:"root"`
	Publish   []string `json:"publish"`
	Subscribe []string `json:"subscribe"`
}

type Key struct {
	ID         string    `json:"id"`
	Algorithm  string    `json:"algorithm"`
	KeyType    string    `json:"keyType"`
	Label      *string   `json:"label"`
	DisabledAt *string   `json:"disabledAt"`
	CreatedAt  string    `json:"createdAt"`
	Scope      *KeyScope `json:"scope"`
	PrivateKey string    `json:"privateKey"`
}

type PrivateKey struct {
	PrivateKey string `json:"privateKey"`
}

type Bucket struct {
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
	Bucket      string `json:"bucket"`
	Destination string `json:"destination"`
	Delivery    bool   `json:"delivery"`
}

type RecordingRule struct {
	ID            string   `json:"id"`
	Label         *string  `json:"label"`
	Patterns      []string `json:"patterns"`
	VodEnabled    bool     `json:"vodEnabled"`
	RetentionDays int64    `json:"retentionDays"`
	BucketID      *string  `json:"bucketId"`
	Bucket        *Bucket  `json:"bucket"`
	Deleting      bool     `json:"deleting"`
	UploadRefusal *string  `json:"uploadRefusal"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

type SyncTarget struct {
	ID                string   `json:"id"`
	Kind              string   `json:"kind"`
	Label             string   `json:"label"`
	PublishPrefixes   []string `json:"publishPrefixes"`
	PublishConfigured bool     `json:"publishConfigured"`
	PublishOrigin     *string  `json:"publishOrigin"`
	CreatedAt         string   `json:"createdAt"`
	UpdatedAt         string   `json:"updatedAt"`
}

type Recording struct {
	ID            string  `json:"id"`
	BroadcastPath string  `json:"broadcastPath"`
	Status        string  `json:"status"`
	StartedAt     string  `json:"startedAt"`
	EndedAt       *string `json:"endedAt"`
	PlaylistURL   *string `json:"playlistUrl"`
	Bytes         float64 `json:"bytes"`
	DurationMs    float64 `json:"durationMs"`
	Deleting      bool    `json:"deleting"`
}

// Usage holds the billable quantities of one period. Optional quantities are
// zero when absent, as the API documents for older history.
type Usage struct {
	PeriodStart          string  `json:"periodStart"`
	PeriodEnd            string  `json:"periodEnd"`
	EgressExternalBytes  float64 `json:"egressExternalBytes"`
	IngressBytes         float64 `json:"ingressBytes"`
	StoredGbSeconds      float64 `json:"storedGbSeconds"`
	VodRecordingBytes    float64 `json:"vodRecordingBytes"`
	VodEgressBytes       float64 `json:"vodEgressBytes"`
	Transcode480Seconds  float64 `json:"transcode480Seconds"`
	Transcode720Seconds  float64 `json:"transcode720Seconds"`
	Transcode1080Seconds float64 `json:"transcode1080Seconds"`
	AgentSeconds         float64 `json:"agentSeconds"`
	CostMicros           float64 `json:"costMicros"`
	Partial              bool    `json:"partial"`
}
