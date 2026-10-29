package runmodel

// Fidelity describes what was recovered, independently of the file extension.
const (
	FidelitySource     = "source"
	FidelityDecompiled = "decompiled-source"
	FidelityPseudocode = "pseudocode"
	FidelityStub       = "stub"
	ProvenanceVersion  = 1
)

type ProvenanceProducer struct {
	Tool    string   `json:"tool"`
	Version string   `json:"version"`
	StepID  string   `json:"stepId"`
	Args    []string `json:"args"`
}

// Identities are scoped by module and inputSha256. Hex locations are strings.
type ProvenanceRecord struct {
	SchemaVersion int                `json:"schemaVersion"`
	ID            string             `json:"id"`
	TargetID      string             `json:"targetId"`
	InputSHA256   string             `json:"inputSha256"`
	ArtifactPath  string             `json:"artifactPath"`
	ArtifactKind  string             `json:"artifactKind"`
	Module        string             `json:"module"`
	Token         *string            `json:"token"`
	RVA           *string            `json:"rva"`
	Address       *string            `json:"address"`
	OriginalName  *string            `json:"originalName"`
	RecoveredName *string            `json:"recoveredName"`
	NameSource    string             `json:"nameSource"`
	Confidence    string             `json:"confidence"`
	Evidence      []Evidence         `json:"evidence"`
	Producer      ProvenanceProducer `json:"producer"`
}
