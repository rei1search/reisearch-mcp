package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rei1search/reisearch-mcp/internal/reisearch"
)

// County tax-record tools (the Taxes tab): get_tax_record, update_tax_record,
// create_tax_record. They wrap the API's "parcel" endpoints; client methods
// in internal/reisearch/parcels.go.

var parcelPKRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

func parcelError(err error) error {
	var apiErr *reisearch.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.Code == "PARCEL_AMBIGUOUS":
		return fmt.Errorf("%w — several parcels share this address. Ask the user which one (or check the candidates) and call get_tax_record again with 'folio'", err)
	case apiErr.Code == "PARCEL_NOT_FOUND":
		return fmt.Errorf("%w — no county record matches. From get_tax_record: check the address (and folio) with the user, and only if it's confirmed missing, create it with create_tax_record. From update_tax_record: the pk/folio are wrong — use exactly the values get_tax_record returned", err)
	case apiErr.Code == "PARCEL_EXISTS":
		return fmt.Errorf("%w — NOTHING was written: the parcel is already stored (see details.existing). Call get_tax_record, then update_tax_record with the returned pk, folio and revision", err)
	case apiErr.Code == "PRECONDITION_FAILED":
		return fmt.Errorf("%w — NOTHING was written: someone else changed this parcel since your revision. Call get_tax_record again, compare your changes with what's stored now, and only then update_tax_record with the new revision. NEVER resend the same patch with just a newer revision — that silently overwrites their change", err)
	case apiErr.Code == "VALIDATION":
		return fmt.Errorf("%w — NOTHING was written. Fix EVERY field listed in details.errors (numbers must be JSON numbers, not strings; unknown fields go under 'additional_details'), then retry; resending the same body fails again", err)
	case apiErr.Status == http.StatusTooManyRequests:
		return fmt.Errorf("%w — rate limited, OR this account's plan isn't enabled for county parcel data. If every parcel call returns this, retrying won't help: tell the user their plan doesn't include parcel access", err)
	case apiErr.Status == http.StatusRequestEntityTooLarge:
		return fmt.Errorf("%w — the request or the resulting record is too large (body max 350 KiB, record max 400 KB, arrays max 500 items)", err)
	case apiErr.Status == http.StatusServiceUnavailable:
		return fmt.Errorf("%w — the parcel store is temporarily unavailable; retry later", err)
	}
	return err
}

// rejectProtected fails when obj sets any of the given top-level keys.
func rejectProtected(obj map[string]any, keys []string, why string) error {
	var bad []string
	for _, k := range keys {
		if _, ok := obj[k]; ok {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("remove %s: %s", strings.Join(bad, ", "), why)
	}
	return nil
}

const parcelWhat = "a property's county TAX RECORD — the data on its Taxes tab: market/assessed/taxable/land/building values, tax districts with millage rates and tax amounts (annual tax), owners, mailing address, legal description, identification (folio/parcel ID, PIN, use code), buildings, sales history, land, permits, extras. One record per county parcel, keyed by address + folio. It is SHARED county data, separate from the user's own property record (get_property_details/update_property). Access is limited to some plans: if EVERY tax-record call returns 429, the plan isn't enabled and retrying won't help."

// ---------------------------------------------------------------- get_tax_record

type GetTaxRecordInput struct {
	PropertyID string `json:"propertyID,omitempty"`
	Address    string `json:"address,omitempty"`
	Unit       string `json:"unit,omitempty"`
	Folio      string `json:"folio,omitempty"`
}

// TaxRecordResult is the lookup result plus which address was searched.
type TaxRecordResult struct {
	reisearch.ParcelResult
	LookedUpAddress string `json:"looked_up_address"`
	PropertyID      string `json:"propertyID,omitempty"`
}

// propertyAddress builds a lookup address from a property's details:
// "street, city, state zip", falling back to its 'location' string.
func propertyAddress(details map[string]any) string {
	d, _ := details["property_detail"].(map[string]any)
	str := func(k string) string { v, _ := d[k].(string); return strings.TrimSpace(v) }
	if street := str("street"); street != "" {
		parts := []string{street}
		if c := str("city"); c != "" {
			parts = append(parts, c)
		}
		if tail := strings.TrimSpace(str("state") + " " + str("zipCode")); tail != "" {
			parts = append(parts, tail)
		}
		return strings.Join(parts, ", ")
	}
	return str("location")
}

// withUnit puts the unit right after the street ("9686 Fontainebleau Blvd 507, Miami, FL 33172").
func withUnit(address, unit string) string {
	unit = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(unit), "#"))
	if unit == "" {
		return address
	}
	street, rest, found := strings.Cut(address, ",")
	if !found {
		return address + " " + unit
	}
	return strings.TrimSpace(street) + " " + unit + "," + rest
}

func (h *PropertyHandler) GetTaxRecord(ctx context.Context, req *mcp.CallToolRequest, input GetTaxRecordInput) (*mcp.CallToolResult, *TaxRecordResult, error) {
	token := TokenFromContext(ctx)
	address := strings.TrimSpace(input.Address)
	propertyID := strings.TrimSpace(input.PropertyID)
	if address == "" && propertyID == "" {
		return nil, nil, fmt.Errorf("give either 'propertyID' (a ReiSearch property — its address is used) or 'address' (e.g. '120 W Comanche Ave, Tampa, FL 33604')")
	}
	if address == "" {
		details, err := h.client.GetPropertyDetails(ctx, token, propertyID)
		if err != nil {
			return nil, nil, fmt.Errorf("couldn't read property %s to get its address: %w", propertyID, err)
		}
		if address = propertyAddress(details); address == "" {
			return nil, nil, fmt.Errorf("property %s has no address on file; ask the user for the address and pass 'address'", propertyID)
		}
	}
	address = withUnit(address, input.Unit)
	if len(address) > 512 {
		return nil, nil, fmt.Errorf("address is too long (max 512 characters)")
	}
	folio := strings.TrimSpace(input.Folio)
	if len(folio) > 64 {
		return nil, nil, fmt.Errorf("folio is too long (max 64 characters)")
	}
	result, err := h.client.LookupParcel(ctx, token, address, folio)
	if err != nil {
		var apiErr *reisearch.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "PARCEL_NOT_FOUND" {
			return nil, nil, fmt.Errorf("%w — no county tax record for %q. If it's a condo/apartment, each unit is its own record and ReiSearch property records don't store the unit: ask the user for the unit number (pass 'unit') or the folio/parcel ID (pass 'folio'). Otherwise check the address; only if it's confirmed missing, create it with create_tax_record", err, address)
		}
		return nil, nil, parcelError(err)
	}
	return nil, &TaxRecordResult{ParcelResult: *result, LookedUpAddress: address, PropertyID: propertyID}, nil
}

// ---------------------------------------------------------------- update_tax_record

type UpdateTaxRecordInput struct {
	PK               string         `json:"pk"`
	Folio            string         `json:"folio"`
	ExpectedRevision int64          `json:"expected_revision"`
	Patch            map[string]any `json:"patch"`
}

func (h *PropertyHandler) UpdateTaxRecord(ctx context.Context, req *mcp.CallToolRequest, input UpdateTaxRecordInput) (*mcp.CallToolResult, *reisearch.ParcelResult, error) {
	pk := strings.TrimSpace(input.PK)
	if !parcelPKRe.MatchString(pk) {
		return nil, nil, fmt.Errorf("pk must be the 32-character lowercase hex 'pk' returned by get_tax_record — never compute it yourself")
	}
	folio := strings.TrimSpace(input.Folio)
	if folio == "" {
		return nil, nil, fmt.Errorf("folio is required: the 'folio' returned by get_tax_record")
	}
	if input.ExpectedRevision < 0 {
		return nil, nil, fmt.Errorf("expected_revision must be the 'revision' returned by get_tax_record or the last update_tax_record (0 for a never-edited county record)")
	}
	if len(input.Patch) == 0 {
		return nil, nil, fmt.Errorf("patch is required: an object with at least one field to change")
	}
	if err := rejectProtected(input.Patch, reisearch.ParcelImmutableFields, "they identify the parcel and can never be changed"); err != nil {
		return nil, nil, err
	}
	if err := rejectProtected(input.Patch, reisearch.ParcelReadOnlyFields, "they are managed by the server"); err != nil {
		return nil, nil, err
	}
	result, err := h.client.PatchParcel(ctx, TokenFromContext(ctx), pk, folio, input.ExpectedRevision, input.Patch)
	if err != nil {
		return nil, nil, parcelError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- create_tax_record

type CreateTaxRecordInput struct {
	Parcel map[string]any `json:"parcel"`
}

func (h *PropertyHandler) CreateTaxRecord(ctx context.Context, req *mcp.CallToolRequest, input CreateTaxRecordInput) (*mcp.CallToolResult, *reisearch.ParcelResult, error) {
	if len(input.Parcel) == 0 {
		return nil, nil, fmt.Errorf("parcel is required: the full record (folio_number, property_address, source, …)")
	}
	if err := rejectProtected(input.Parcel, reisearch.ParcelReadOnlyFields, "they are set by the server on create"); err != nil {
		return nil, nil, err
	}
	result, err := h.client.CreateParcel(ctx, TokenFromContext(ctx), input.Parcel)
	if err != nil {
		var apiErr *reisearch.APIError
		if !errors.As(err, &apiErr) {
			return nil, nil, fmt.Errorf("%w — the parcel may still have been created; call get_tax_record before retrying", err)
		}
		return nil, nil, parcelError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- registration

const taxRecordTypedFields = "Typed fields: legal_description, lat, lon, data_version ('MM.DD.YYYY'), mailing_address {line1, line2, city, state, zip, country, formatted_address}, owners [{name}], identification {folio, primary_id, primary_id_type, pin, strap, subdivision_id, municipality_code, neighborhood_code, property_use_code, is_inactive, alt_ids}, " +
	"valuation {market_value, assessed_value, taxable_value, land_value, building_value, tax_authorities_breakdown [{authority_name, authority_type, millage_rate, tax_amount}]}, buildings [{bedrooms, bathrooms, year_built, effective_year_built, heated_sqft, gross_sqft, total_stories, total_units, addresses, sub_areas, structural_elements}], " +
	"sales [{date 'YYYY-MM-DD', price, grantor, grantee, book_page, instrument_number, instrument_type, qualified, reason_code, vacant_improved}], extras {acreage, total_buildings, county_last_edit_date 'YYYY-MM-DD'}, land [..], permits [..], additional_site_addresses [..], source {county, state}. " +
	"Numbers must be JSON numbers, never strings; bedrooms, year_built, effective_year_built, total_units and total_buildings must be WHOLE numbers (round scraped values). Arrays max 500 items. Unknown keys are rejected — put anything else under 'additional_details' (free-form). " +
	"There is NO typed field for multi-year tax history: store it under additional_details (the Taxes tab doesn't display it yet). " +
	"valuation.tax_authorities_breakdown is what fills the Taxes tab's annual tax, effective millage and tax-district cards (they show the sums of tax_amount and millage_rate), so include EVERY taxing authority."

func registerParcelTools(server *mcp.Server, h *PropertyHandler) {
	destructive, notDestructive := true, false

	mcp.AddTool(server, &mcp.Tool{Name: "get_tax_record", Description: "Get " + parcelWhat + " Pass EITHER 'propertyID' (a ReiSearch property — its street/city/state/zip are used) OR 'address' (free text, e.g. '120 W Comanche Ave, Tampa, FL 33604'); if both are given, 'address' wins. Optional 'unit' (condo/apartment unit, added after the street) and 'folio' (county parcel ID — the most exact way to pick a record; required when an address has several). " +
		"Condos: every unit is its own record and ReiSearch property records don't store unit numbers, so for a condo you need 'unit' or 'folio'. Read-only. " +
		"Returns { pk, sk, folio, revision, matched_by, record, looked_up_address, propertyID }. Keep pk, folio and revision: update_tax_record needs all three ('revision' is passed as expected_revision; 0 = a county-loaded record never edited through the API). 'record' is returned as stored; county-loaded records may hold numeric strings or extra keys. " +
		"On the Taxes tab, annual tax, effective millage and the district cards come from record.valuation.tax_authorities_breakdown; exemptions = market − taxable. " +
		"Errors: PARCEL_AMBIGUOUS (several records at the address; details.candidates) → retry with 'folio'; PARCEL_NOT_FOUND → for a condo ask for the unit or folio; otherwise check the address, and only if confirmed missing, create_tax_record.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, h.GetTaxRecord)

	mcp.AddTool(server, &mcp.Tool{Name: "update_tax_record", Description: "Change fields on " + parcelWhat + " Changes are visible to everyone, so CONFIRM the values and their source with the user first. Requires 'pk', 'folio' and 'expected_revision' — exactly as returned by the latest get_tax_record or update_tax_record (never compute pk) — and 'patch', a JSON merge patch: " +
		"objects are merged (keys you send update, others are kept); any other value INCLUDING AN ARRAY replaces the stored value (to change one sale/building/tax district, send the WHOLE array); null DELETES the field; omitted fields are unchanged. " +
		"Cannot change folio_number, property_address or ingested_at, or server fields (PK, SK, hashes, revision, api_*). " + taxRecordTypedFields + " " +
		"Returns { pk, folio, revision (the NEW one — use it for the next update), fields { updated, removed }, record }; revision rises even if nothing changed. " +
		"Errors: PRECONDITION_FAILED (412) → someone changed it: get_tax_record again, reconcile, then update with the new revision — NEVER resend the same patch with a newer revision; VALIDATION → fix every field in details.errors; PARCEL_NOT_FOUND → wrong pk/folio.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive}}, h.UpdateTaxRecord)

	mcp.AddTool(server, &mcp.Tool{Name: "create_tax_record", Description: "Create " + parcelWhat + " ONLY after get_tax_record returned PARCEL_NOT_FOUND (for a condo: with the unit or folio) and the user confirmed the data — records can't be deleted. Requires 'parcel', the full record: folio_number (max 64 chars, no '#'), property_address {line1 (include the unit for a condo), city, state (2 capital letters), zip (5-digit or ZIP+4), country, formatted_address}, source {county, state}; " +
		"optional: data_version ('MM.DD.YYYY', defaults to today), ingested_at (RFC 3339, defaults to now), and the same typed fields as update_tax_record. " + taxRecordTypedFields + " " +
		"Do NOT send PK, SK, hashes, revision or api_* — the server sets them. " +
		"Returns { pk, sk, folio, revision: 1, record }. Errors: PARCEL_EXISTS → NOTHING written; call get_tax_record and use update_tax_record instead; VALIDATION → fix every field in details.errors. Not safe to retry blindly after a timeout — call get_tax_record first.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &notDestructive}}, h.CreateTaxRecord)
}
