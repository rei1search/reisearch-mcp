package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

// Underwriting v6 tools. Every tool takes propertyID + strategy; writes pick a
// version with versionId or create_new. Client methods live in
// internal/reisearch/underwriting.go.

// ---------------------------------------------------------------- shared validation

func validateStrategy(strategy string) error {
	if strategy == "" {
		return fmt.Errorf("strategy is required: 'coLiving' or 'fixFlip' (ask the user which strategy if they haven't said)")
	}
	if !slices.Contains(reisearch.UnderwritingStrategies, strategy) {
		return fmt.Errorf("invalid strategy %q: must be 'coLiving' or 'fixFlip' (case-sensitive)", strategy)
	}
	return nil
}

// validateVersionSelection enforces exactly one of versionID / createNew, and
// a version name with createNew when nameRequired. The *Field args name the
// inputs in error messages (they differ between tools).
func validateVersionSelection(versionID string, createNew bool, versionName string, nameRequired bool, idField, newField, nameField string) error {
	switch {
	case versionID != "" && createNew:
		return fmt.Errorf("pass either '%s' or '%s': true, not both", idField, newField)
	case versionID == "" && !createNew:
		return fmt.Errorf("choose a version: pass '%s' (an existing version — see list_underwriting_versions) or '%s': true to start a new version", idField, newField)
	case createNew && nameRequired && strings.TrimSpace(versionName) == "":
		return fmt.Errorf("'%s' is required with '%s': true", nameField, newField)
	}
	if len(versionName) > 100 {
		return fmt.Errorf("'%s' cannot exceed 100 characters", nameField)
	}
	return nil
}

func validateSection(strategy, section string) error {
	if section == "" {
		return fmt.Errorf("section is required; for %s use one of: %s", strategy, strings.Join(reisearch.UnderwritingSectionsFor(strategy), ", "))
	}
	if _, ok := reisearch.UnderwritingSectionPaths[strategy][section]; !ok {
		return fmt.Errorf("section %q is not supported for strategy %s; use one of: %s", section, strategy, strings.Join(reisearch.UnderwritingSectionsFor(strategy), ", "))
	}
	return nil
}

func validatePath(strategy, section, path string) error {
	allowed := reisearch.UnderwritingSectionPaths[strategy][section]
	if !slices.Contains(allowed, path) {
		return fmt.Errorf("invalid path %q for section %q; allowed paths: %s", path, section, strings.Join(allowed, ", "))
	}
	return nil
}

func validateRoomTarget(area, tag string) error {
	if !slices.Contains(reisearch.UnderwritingRoomAreas, area) {
		return fmt.Errorf("invalid area %q; use one of: %s", area, strings.Join(reisearch.UnderwritingRoomAreas, ", "))
	}
	if tag == "" || len(tag) > 256 {
		return fmt.Errorf("tag is required (1–256 bytes), e.g. 'kitchen:1', 'bedroom:2', 'custom:<id>:1'")
	}
	return nil
}

func requireVersionID(versionID string) error {
	if versionID == "" {
		return fmt.Errorf("versionId is required (get it from list_underwriting_versions)")
	}
	return nil
}

// uwError adds a next-step hint to known underwriting API failures.
func uwError(err error) error {
	var apiErr *reisearch.UnderwritingAPIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.Status == http.StatusConflict:
		return fmt.Errorf("%w — this section already exists in that version; use update_underwriting_section to change it", err)
	case apiErr.Status == http.StatusNotFound && strings.Contains(apiErr.Message, "version"):
		return fmt.Errorf("%w — versions are separate per strategy, so this may be a strategy mismatch (coLiving vs fixFlip); check the strategy with list_underwriting_versions before telling the user the version is gone", err)
	case apiErr.Status == http.StatusForbidden:
		return fmt.Errorf("%w — the user lacks permission on this property, or the version belongs to another user (only its creator can change it)", err)
	}
	return err
}

// nonIdempotentError warns against blind retries when a create-type call
// failed without a definite API answer (e.g. a timeout).
func nonIdempotentError(err error) error {
	var apiErr *reisearch.UnderwritingAPIError
	if errors.As(err, &apiErr) {
		return uwError(err)
	}
	return fmt.Errorf("%w — the request may still have created a version; call list_underwriting_versions before retrying, or you may create a duplicate", err)
}

// ---------------------------------------------------------------- list_underwriting_versions

type ListUnderwritingVersionsInput struct {
	PropertyID string `json:"propertyID"`
	Strategy   string `json:"strategy"`
	Scope      string `json:"scope,omitempty"`
}

func (h *PropertyHandler) ListUnderwritingVersions(ctx context.Context, req *mcp.CallToolRequest, input ListUnderwritingVersionsInput) (*mcp.CallToolResult, *reisearch.UnderwritingVersionList, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	scope := input.Scope
	if scope == "" {
		scope = "mine" // tool default; the API's own default is "all"
	}
	if scope != "mine" && scope != "all" {
		return nil, nil, fmt.Errorf("scope must be 'mine' or 'all'")
	}

	result, err := h.client.ListUnderwritingVersions(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, scope)
	if err != nil {
		return nil, nil, uwError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- get_underwriting_section

type GetUnderwritingSectionInput struct {
	PropertyID string `json:"propertyID"`
	Strategy   string `json:"strategy"`
	Section    string `json:"section"`
	VersionID  string `json:"versionId,omitempty"`
}

type GetUnderwritingSectionOutput struct {
	Found     bool                   `json:"found"`
	VersionID string                 `json:"versionId,omitempty"`
	Section   string                 `json:"section"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Message   string                 `json:"message,omitempty"`
}

func (h *PropertyHandler) GetUnderwritingSection(ctx context.Context, req *mcp.CallToolRequest, input GetUnderwritingSectionInput) (*mcp.CallToolResult, *GetUnderwritingSectionOutput, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := validateSection(input.Strategy, input.Section); err != nil {
		return nil, nil, err
	}

	data, err := h.client.GetUnderwritingSection(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, input.Section, input.VersionID)
	if err != nil {
		return nil, nil, uwError(err)
	}
	out := &GetUnderwritingSectionOutput{VersionID: input.VersionID, Section: input.Section}
	if data == nil {
		out.Message = "No data has been written to this section yet (this is not an error)."
		return nil, out, nil
	}
	out.Found = true
	out.Data = data
	return nil, out, nil
}

// ---------------------------------------------------------------- get_all_underwriting

type GetAllUnderwritingInput struct {
	PropertyID string `json:"propertyID"`
	Strategy   string `json:"strategy"`
}

type GetAllUnderwritingOutput struct {
	Count   int                      `json:"count"`
	Entries []map[string]interface{} `json:"entries"`
}

func (h *PropertyHandler) GetAllUnderwriting(ctx context.Context, req *mcp.CallToolRequest, input GetAllUnderwritingInput) (*mcp.CallToolResult, *GetAllUnderwritingOutput, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}

	entries, err := h.client.GetAllUnderwriting(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy)
	if err != nil {
		return nil, nil, uwError(err)
	}
	return nil, &GetAllUnderwritingOutput{Count: len(entries), Entries: entries}, nil
}

// ---------------------------------------------------------------- create_underwriting_section

type CreateUnderwritingSectionInput struct {
	PropertyID  string                 `json:"propertyID"`
	Strategy    string                 `json:"strategy"`
	Section     string                 `json:"section"`
	Data        map[string]interface{} `json:"data"`
	VersionID   string                 `json:"versionId,omitempty"`
	CreateNew   bool                   `json:"create_new,omitempty"`
	VersionName string                 `json:"version_name,omitempty"`
}

func (h *PropertyHandler) CreateUnderwritingSection(ctx context.Context, req *mcp.CallToolRequest, input CreateUnderwritingSectionInput) (*mcp.CallToolResult, *reisearch.CreateSectionResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := validateSection(input.Strategy, input.Section); err != nil {
		return nil, nil, err
	}
	if len(input.Data) == 0 {
		return nil, nil, fmt.Errorf("data is required: an object with the section's fields")
	}
	// The create route silently drops unknown keys, so catch typos here.
	for key := range input.Data {
		if err := validatePath(input.Strategy, input.Section, key); err != nil {
			return nil, nil, fmt.Errorf("data has an unknown field: %w", err)
		}
	}
	if err := validateVersionSelection(input.VersionID, input.CreateNew, input.VersionName, false, "versionId", "create_new", "version_name"); err != nil {
		return nil, nil, err
	}

	sel := reisearch.VersionSelector{VersionID: input.VersionID, CreateNew: input.CreateNew}
	if input.CreateNew {
		sel.VersionName = input.VersionName
	}
	result, err := h.client.CreateUnderwritingSection(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, input.Section, sel, input.Data)
	if err != nil {
		if input.CreateNew {
			return nil, nil, nonIdempotentError(err)
		}
		return nil, nil, uwError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- update_underwriting_section

type UpdateUnderwritingSectionInput struct {
	PropertyID string      `json:"propertyID"`
	Strategy   string      `json:"strategy"`
	Section    string      `json:"section"`
	Path       string      `json:"path"`
	Value      interface{} `json:"value"`
	VersionID  string      `json:"versionId,omitempty"`
	CreateNew  bool        `json:"create_new,omitempty"`
}

func (h *PropertyHandler) UpdateUnderwritingSection(ctx context.Context, req *mcp.CallToolRequest, input UpdateUnderwritingSectionInput) (*mcp.CallToolResult, *reisearch.UpdateSectionResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := validateSection(input.Strategy, input.Section); err != nil {
		return nil, nil, err
	}
	if err := validatePath(input.Strategy, input.Section, input.Path); err != nil {
		return nil, nil, err
	}
	if input.Value == nil {
		return nil, nil, fmt.Errorf("value is required and cannot be null")
	}
	if m, ok := input.Value.(map[string]interface{}); ok && len(m) == 0 {
		return nil, nil, fmt.Errorf("value cannot be an empty object")
	}
	if err := validateVersionSelection(input.VersionID, input.CreateNew, "", false, "versionId", "create_new", ""); err != nil {
		return nil, nil, err
	}

	sel := reisearch.VersionSelector{VersionID: input.VersionID, CreateNew: input.CreateNew}
	result, err := h.client.UpdateUnderwritingSection(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, input.Section, input.Path, sel, input.Value)
	if err != nil {
		if input.CreateNew {
			return nil, nil, nonIdempotentError(err)
		}
		return nil, nil, uwError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- upsert_underwriting_room

type UpsertUnderwritingRoomInput struct {
	PropertyID  string                     `json:"propertyID"`
	Strategy    string                     `json:"strategy"`
	Area        string                     `json:"area"`
	Tag         string                     `json:"tag"`
	Room        reisearch.UnderwritingRoom `json:"room"`
	VersionID   string                     `json:"versionId,omitempty"`
	CreateNew   bool                       `json:"create_new,omitempty"`
	VersionName string                     `json:"version_name,omitempty"`
}

func (h *PropertyHandler) UpsertUnderwritingRoom(ctx context.Context, req *mcp.CallToolRequest, input UpsertUnderwritingRoomInput) (*mcp.CallToolResult, *reisearch.RoomResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := validateRoomTarget(input.Area, input.Tag); err != nil {
		return nil, nil, err
	}
	if raw, _ := json.Marshal(input.Room); string(raw) == "{}" {
		return nil, nil, fmt.Errorf("room cannot be empty: set at least one of name, source, squareFootage, hasEnSuite, cost, lineItems, ai")
	}
	for i, item := range input.Room.LineItems {
		if strings.TrimSpace(item.Name) == "" {
			return nil, nil, fmt.Errorf("room.lineItems[%d].name is required", i)
		}
	}
	if err := validateVersionSelection(input.VersionID, input.CreateNew, input.VersionName, true, "versionId", "create_new", "version_name"); err != nil {
		return nil, nil, err
	}

	sel := reisearch.VersionSelector{VersionID: input.VersionID, CreateNew: input.CreateNew}
	if input.CreateNew {
		sel.VersionName = input.VersionName
	}
	result, err := h.client.UpsertUnderwritingRoom(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, input.Area, input.Tag, sel, input.Room)
	if err != nil {
		if input.CreateNew {
			return nil, nil, nonIdempotentError(err)
		}
		return nil, nil, uwError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- delete_underwriting_room

type DeleteUnderwritingRoomInput struct {
	PropertyID string `json:"propertyID"`
	Strategy   string `json:"strategy"`
	Area       string `json:"area"`
	Tag        string `json:"tag"`
	VersionID  string `json:"versionId,omitempty"`
}

func (h *PropertyHandler) DeleteUnderwritingRoom(ctx context.Context, req *mcp.CallToolRequest, input DeleteUnderwritingRoomInput) (*mcp.CallToolResult, *reisearch.DeleteRoomResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := validateRoomTarget(input.Area, input.Tag); err != nil {
		return nil, nil, err
	}

	result, err := h.client.DeleteUnderwritingRoom(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, input.Area, input.Tag, input.VersionID)
	if err != nil {
		return nil, nil, uwError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- clone_underwriting_version

type CloneUnderwritingVersionInput struct {
	PropertyID  string `json:"propertyID"`
	Strategy    string `json:"strategy"`
	VersionID   string `json:"versionId"`
	VersionName string `json:"version_name,omitempty"`
}

func (h *PropertyHandler) CloneUnderwritingVersion(ctx context.Context, req *mcp.CallToolRequest, input CloneUnderwritingVersionInput) (*mcp.CallToolResult, *reisearch.CloneVersionResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := requireVersionID(input.VersionID); err != nil {
		return nil, nil, err
	}
	if len(input.VersionName) > 100 {
		return nil, nil, fmt.Errorf("version_name cannot exceed 100 characters")
	}

	result, err := h.client.CloneUnderwritingVersion(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, input.VersionID, input.VersionName)
	if err != nil {
		return nil, nil, nonIdempotentError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- rename_underwriting_version

type RenameUnderwritingVersionInput struct {
	PropertyID  string `json:"propertyID"`
	Strategy    string `json:"strategy"`
	VersionID   string `json:"versionId"`
	VersionName string `json:"version_name"`
}

func (h *PropertyHandler) RenameUnderwritingVersion(ctx context.Context, req *mcp.CallToolRequest, input RenameUnderwritingVersionInput) (*mcp.CallToolResult, *reisearch.VersionChangeResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := requireVersionID(input.VersionID); err != nil {
		return nil, nil, err
	}
	name := strings.TrimSpace(input.VersionName)
	if name == "" {
		return nil, nil, fmt.Errorf("version_name is required")
	}
	if len(name) > 100 {
		return nil, nil, fmt.Errorf("version_name cannot exceed 100 characters")
	}

	token := TokenFromContext(ctx)
	// The API answers 500 "name unchanged" for a no-op rename, so skip it.
	if list, err := h.client.ListUnderwritingVersions(ctx, token, input.PropertyID, input.Strategy, "mine"); err == nil {
		for _, v := range list.Versions {
			if v.VersionID == input.VersionID && v.VersionName == name {
				return nil, &reisearch.VersionChangeResult{
					PropertyID:  input.PropertyID,
					VersionID:   input.VersionID,
					VersionName: name,
					Skipped:     true,
					Message:     "Version already has this name; nothing changed.",
				}, nil
			}
		}
	}

	result, err := h.client.RenameUnderwritingVersion(ctx, token, input.PropertyID, input.Strategy, input.VersionID, name)
	if err != nil {
		return nil, nil, uwError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- set_underwriting_deal_score

type SetUnderwritingDealScoreInput struct {
	PropertyID string   `json:"propertyID"`
	Strategy   string   `json:"strategy"`
	VersionID  string   `json:"versionId"`
	DealScore  *float64 `json:"deal_score"`
}

func (h *PropertyHandler) SetUnderwritingDealScore(ctx context.Context, req *mcp.CallToolRequest, input SetUnderwritingDealScoreInput) (*mcp.CallToolResult, *reisearch.VersionChangeResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := requireVersionID(input.VersionID); err != nil {
		return nil, nil, err
	}
	if input.DealScore == nil {
		return nil, nil, fmt.Errorf("deal_score is required (0–100)")
	}
	if *input.DealScore < 0 || *input.DealScore > 100 {
		return nil, nil, fmt.Errorf("deal_score must be between 0 and 100")
	}

	result, err := h.client.SetUnderwritingDealScore(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, input.VersionID, *input.DealScore)
	if err != nil {
		return nil, nil, uwError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- delete_underwriting_version

type DeleteUnderwritingVersionInput struct {
	PropertyID string `json:"propertyID"`
	Strategy   string `json:"strategy"`
	VersionID  string `json:"versionId"`
}

func (h *PropertyHandler) DeleteUnderwritingVersion(ctx context.Context, req *mcp.CallToolRequest, input DeleteUnderwritingVersionInput) (*mcp.CallToolResult, *reisearch.VersionChangeResult, error) {
	if input.PropertyID == "" {
		return nil, nil, fmt.Errorf("propertyID is required")
	}
	if err := validateStrategy(input.Strategy); err != nil {
		return nil, nil, err
	}
	if err := requireVersionID(input.VersionID); err != nil {
		return nil, nil, err
	}

	result, err := h.client.DeleteUnderwritingVersion(ctx, TokenFromContext(ctx), input.PropertyID, input.Strategy, input.VersionID)
	if err != nil {
		return nil, nil, uwError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- registration

// Shared description fragments, so every tool tells the model the same rules.
const (
	uwStrategyNote = " Every call requires 'propertyID' and 'strategy' ('coLiving' or 'fixFlip'); coLiving and fixFlip data are kept completely separate. If the user hasn't said which strategy, ASK — don't guess."
	uwVersionNote  = " Underwriting data lives in named, per-user versions. Store the 'versionId' a write returns and pass it as 'versionId' on every later call; omitting it on reads falls back to the user's NEWEST version, which changes after any create or clone."
	uwWriteSelect  = " Choose the version: EITHER 'versionId' (existing version) OR 'create_new': true (start a new version) — never both."
)

func registerUnderwritingTools(server *mcp.Server, h *PropertyHandler) {
	destructive := true

	mcp.AddTool(server, &mcp.Tool{Name: "list_underwriting_versions", Description: "List the underwriting versions on a property, newest-relevant metadata included (versionId, versionName, dealScore, sourceVersionId for clones, createdById, and createdAt/updatedAt as epoch milliseconds plus ISO strings)." + uwStrategyNote + " Optional 'scope': 'mine' (default — the current user's own versions) or 'all' (includes collaborators' versions). Use this to find the versionId to pass to other underwriting tools, and before retrying any write that timed out. 'count': 0 means no versions yet for this strategy."}, h.ListUnderwritingVersions)

	mcp.AddTool(server, &mcp.Tool{Name: "get_underwriting_section", Description: "Read one underwriting section of a property for the current user." + uwStrategyNote + " Requires 'section' — both strategies: postRenovation, repairRenovation, appliances, holdingCost, fundingSource; coLiving only: coliving, furnishing, operatingRevenues, operatingExpenses, privateMoneyFinancing; fixFlip only: saleExit." + uwVersionNote + " Returns { found, section, data }. found:false means nothing has been written to that section yet — this is NOT an error."}, h.GetUnderwritingSection)

	mcp.AddTool(server, &mcp.Tool{Name: "get_all_underwriting", Description: "Read ALL underwriting on a property for one strategy, across every user and version — this INCLUDES COLLABORATORS' financial work, not just the current user's. Use it only when the user wants to see or compare everyone's underwriting; for the user's own data use list_underwriting_versions (scope 'mine') and get_underwriting_section." + uwStrategyNote + " Returns { count, entries }: one entry per (creator, version) with user_id, versionId, versionName, versionMetadata, and 'underwriting' (section name → section data). Label entries by versionName and owner when presenting them."}, h.GetAllUnderwriting)

	mcp.AddTool(server, &mcp.Tool{Name: "create_underwriting_section", Description: "Create an underwriting section in a version. PREFER update_underwriting_section for normal edits — it upserts, while this tool FAILS with 409 once the section exists in that version. Use this mainly to start a new version with a full section ('create_new': true, optional 'version_name' ≤100 chars; default names are '1', '2', …)." + uwStrategyNote + uwWriteSelect + " Requires 'section' (see get_underwriting_section for the list) and 'data': an object whose top-level keys are the section's fields — field names must match the schema exactly (see the allowed paths in update_underwriting_section's description; unknown keys are rejected). Returns { versionId, versionName, section, data } — store the versionId. Not idempotent with create_new: if a call times out, call list_underwriting_versions before retrying."}, h.CreateUnderwritingSection)

	mcp.AddTool(server, &mcp.Tool{Name: "update_underwriting_section", Description: "Write one top-level field ('path') of an underwriting section — the DEFAULT way to change underwriting. Creates the section if it doesn't exist yet." + uwStrategyNote + uwWriteSelect + " Requires 'section', 'path', and 'value' (any non-null JSON). Allowed paths per section — postRenovation: bedrooms, bathrooms, sqft, aduPrivateSuite | repairRenovation: interiorRenovations, aduAdditionDetails, hvacDetails, electricalCost, plumbingCost, exteriorRenovations, repairContingency, estimatedTotal, calculatedTotal | appliances: refrigerator, miniFridge, washerDryer, waterHeater, stove, microwave, airFryer, toaster, calculatedTotal, estimatedTotal | holdingCost: total, holdingPeriod, unitAmount | fundingSource: debtFinancing, cashEquity, creativeFinance | coliving (coLiving): securityCameras, smartLocks, smartThermostat, doorSensors, itCloset, propertyPictures, complianceItems, calculatedTotal, estimatedTotal | furnishing (coLiving): bedroomsFurnishing, bathroomsFurnishing, kitchenFurnishing, commonAreasFurnishing, cleaningSupplies, calculatedTotal, estimatedTotal | operatingRevenues (coLiving): totalBeds, totalBaths, rentalRevenue, rentalLateFee, sharedLaundryFee, miscellaneousIncome, movingFee, calculatedTotal, estimatedTotal | operatingExpenses (coLiving): bookingServiceFee, utilities, cleaningService, maintenance, management, totalOperatingExpenses | privateMoneyFinancing (coLiving): privateMoneyLender, privateMoneyPartner, hardMoneyLender, calculatedTotal, estimatedTotal | saleExit (fixFlip): salePrice, agentCommission, sellerClosingCost. MERGE RULES: an object value deep-merges (sibling fields are kept); arrays and scalars REPLACE — so for arrays (lineItems, rentalRevenue, loan lists) read the section first, modify the array, and write it back whole. Nested field names must match the schema exactly: with an existing versionId, misspelled keys are stored as-is rather than rejected. Returns { versionId, section, path, data } where data echoes what you sent, NOT the merged section — use get_underwriting_section to confirm the result. For per-room repair data prefer upsert_underwriting_room."}, h.UpdateUnderwritingSection)

	mcp.AddTool(server, &mcp.Tool{Name: "upsert_underwriting_room", Description: "Create or update one room's repair estimate inside repairRenovation (stored at interiorRenovations.{area}.items.{tag})." + uwStrategyNote + uwWriteSelect + " With 'create_new': true, 'version_name' is REQUIRED. Requires 'area' (bedrooms, bathrooms, kitchen, livingRoom, diningRoom, laundry, fireplace, attic, basement, generalUpdates, miscellaneous), 'tag' (the room tag, e.g. 'kitchen:1', 'bedroom:2', 'custom:<id>:1' — the same tag_type used for renovation images), and 'room' with any of: name, source ('ai_estimate' | 'manual'), squareFootage, hasEnSuite, cost {materialCost, laborCost, sqft, total}, lineItems [{name, cost {materialCost, laborCost, sqft, total}}], ai {threadId, estimateId, generatedImageS3Key?, finishGrade?, rehabLevel?}. Costs ALWAYS go inside a 'cost' object (no top-level materialCost). Only the room fields you send change — e.g. sending only 'cost' keeps the existing lineItems — but 'lineItems' replaces the whole list when sent. Returns { versionId, room } where room echoes your patch, not the merged room."}, h.UpsertUnderwritingRoom)

	mcp.AddTool(server, &mcp.Tool{Name: "delete_underwriting_room", Description: "Delete one room from repairRenovation. CONFIRM with the user before calling — this permanently removes the room's estimate." + uwStrategyNote + " Requires 'area' and 'tag' (see upsert_underwriting_room). Pass 'versionId' — strongly recommended; if omitted the room is deleted from the user's NEWEST version, which may not be the one you mean. Returns { tag }. 'room not found' means that room isn't in that version.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive}}, h.DeleteUnderwritingRoom)

	mcp.AddTool(server, &mcp.Tool{Name: "clone_underwriting_version", Description: "Copy an entire underwriting version (every section) into a NEW version — use it to explore a what-if scenario without touching the original." + uwStrategyNote + " Requires 'versionId' (the source version); optional 'version_name' (≤100 chars). Returns { sourceVersionId, versionId, versionName, cloned: true } — the NEW versionId is the one to use for further edits. Not idempotent: every call makes another copy, so if a call times out, call list_underwriting_versions before retrying."}, h.CloneUnderwritingVersion)

	mcp.AddTool(server, &mcp.Tool{Name: "rename_underwriting_version", Description: "Rename one of the current user's underwriting versions (only the version's creator can rename it)." + uwStrategyNote + " Requires 'versionId' and 'version_name' (≤100 chars). Renaming to the current name succeeds without changing anything (skipped: true). Returns { propertyId, versionId, versionName }."}, h.RenameUnderwritingVersion)

	mcp.AddTool(server, &mcp.Tool{Name: "set_underwriting_deal_score", Description: "Set the deal score (0–100, decimals allowed, e.g. 82.5) on one of the current user's underwriting versions." + uwStrategyNote + " Requires 'versionId' and 'deal_score'. Returns { propertyId, versionId, dealScore }."}, h.SetUnderwritingDealScore)

	mcp.AddTool(server, &mcp.Tool{Name: "delete_underwriting_version", Description: "Permanently delete an entire underwriting version, including EVERY section in it. CONFIRM with the user before calling — name the version (versionName) you are about to delete; this cannot be undone." + uwStrategyNote + " Requires 'versionId' (only the version's creator can delete it). Returns { propertyId, versionId }. A 404 may mean the version is under the other strategy.", Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive}}, h.DeleteUnderwritingVersion)
}
