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

// County parcel tools (the Taxes tab): get_parcel, update_parcel,
// create_parcel. Client methods in internal/reisearch/parcels.go.

var parcelPKRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

func parcelError(err error) error {
	var apiErr *reisearch.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.Code == "PARCEL_AMBIGUOUS":
		return fmt.Errorf("%w — several parcels share this address. Ask the user which one (or check the candidates) and call get_parcel again with 'folio'", err)
	case apiErr.Code == "PARCEL_NOT_FOUND":
		return fmt.Errorf("%w — no county record matches. From get_parcel: check the address (and folio) with the user, and only if it's confirmed missing, create it with create_parcel. From update_parcel: the pk/folio are wrong — use exactly the values get_parcel returned", err)
	case apiErr.Code == "PARCEL_EXISTS":
		return fmt.Errorf("%w — NOTHING was written: the parcel is already stored (see details.existing). Call get_parcel, then update_parcel with the returned pk, folio and revision", err)
	case apiErr.Code == "PRECONDITION_FAILED":
		return fmt.Errorf("%w — NOTHING was written: someone else changed this parcel since your revision. Call get_parcel again, compare your changes with what's stored now, and only then update_parcel with the new revision. NEVER resend the same patch with just a newer revision — that silently overwrites their change", err)
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

const parcelWhat = "a county tax/assessment parcel record — the data on a property's Taxes tab: market/assessed/taxable/land/building values, tax districts with millage rates and tax amounts, owners, mailing address, legal description, identification (folio, PIN, use code), buildings, sales history, land, permits, extras. It is SHARED county data (not the user's own property record, which is get_property_details/update_property). Access is limited to some plans: if EVERY parcel call returns 429, the plan isn't enabled and retrying won't help."

// ---------------------------------------------------------------- get_parcel

type GetParcelInput struct {
	Address string `json:"address"`
	Folio   string `json:"folio,omitempty"`
}

func (h *PropertyHandler) GetParcel(ctx context.Context, req *mcp.CallToolRequest, input GetParcelInput) (*mcp.CallToolResult, *reisearch.ParcelResult, error) {
	address := strings.TrimSpace(input.Address)
	if address == "" {
		return nil, nil, fmt.Errorf("address is required: the full property address, e.g. '120 W Comanche Ave, Tampa, FL 33604'")
	}
	if len(address) > 512 {
		return nil, nil, fmt.Errorf("address is too long (max 512 characters)")
	}
	folio := strings.TrimSpace(input.Folio)
	if len(folio) > 64 {
		return nil, nil, fmt.Errorf("folio is too long (max 64 characters)")
	}
	result, err := h.client.LookupParcel(ctx, TokenFromContext(ctx), address, folio)
	if err != nil {
		return nil, nil, parcelError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- update_parcel

type UpdateParcelInput struct {
	PK               string         `json:"pk"`
	Folio            string         `json:"folio"`
	ExpectedRevision int64          `json:"expected_revision"`
	Patch            map[string]any `json:"patch"`
}

func (h *PropertyHandler) UpdateParcel(ctx context.Context, req *mcp.CallToolRequest, input UpdateParcelInput) (*mcp.CallToolResult, *reisearch.ParcelResult, error) {
	pk := strings.TrimSpace(input.PK)
	if !parcelPKRe.MatchString(pk) {
		return nil, nil, fmt.Errorf("pk must be the 32-character lowercase hex 'pk' returned by get_parcel — never compute it yourself")
	}
	folio := strings.TrimSpace(input.Folio)
	if folio == "" {
		return nil, nil, fmt.Errorf("folio is required: the 'folio' returned by get_parcel")
	}
	if input.ExpectedRevision < 0 {
		return nil, nil, fmt.Errorf("expected_revision must be the 'revision' returned by get_parcel or the last update_parcel (0 for a never-edited county record)")
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

// ---------------------------------------------------------------- create_parcel

type CreateParcelInput struct {
	Parcel map[string]any `json:"parcel"`
}

func (h *PropertyHandler) CreateParcel(ctx context.Context, req *mcp.CallToolRequest, input CreateParcelInput) (*mcp.CallToolResult, *reisearch.ParcelResult, error) {
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
			return nil, nil, fmt.Errorf("%w — the parcel may still have been created; call get_parcel before retrying", err)
		}
		return nil, nil, parcelError(err)
	}
	return nil, result, nil
}

// ---------------------------------------------------------------- registration

func registerParcelTools(server *mcp.Server, h *PropertyHandler) {
	mcp.AddTool(server, &mcp.Tool{Name: "get_parcel", Description: "Look up " + parcelWhat + " Requires 'address' (free text, e.g. '120 W Comanche Ave, Tampa, FL 33604'); optional 'folio' (parcel ID) to pick one parcel when an address has several. Read-only. " +
		"Returns { pk, sk, folio, revision, matched_by, record }. Keep pk, folio and revision: update_parcel needs all three, and 'revision' is the version you pass as expected_revision (0 = a county-loaded record never edited through the API). 'record' is returned as stored; county-loaded records may contain numeric strings or extra keys. " +
		"Valuation and taxes: the Taxes tab's annual tax, effective millage, district breakdown and rates all come from record.valuation.tax_authorities_breakdown (sum of tax_amount / millage_rate); exemptions = market − taxable. " +
		"Errors: PARCEL_AMBIGUOUS (several parcels at the address; details.candidates) → retry with 'folio'; PARCEL_NOT_FOUND → only after confirming the address, create_parcel."}, h.GetParcel)

	mcp.AddTool(server, &mcp.Tool{Name: "update_parcel", Description: "Change fields on " + parcelWhat + " Changes are visible to everyone, so CONFIRM the values and their source with the user first. Requires 'pk', 'folio' and 'expected_revision' — exactly as returned by the latest get_parcel or update_parcel (never compute pk) — and 'patch', a JSON merge patch: " +
		"objects are merged (keys you send update, others are kept); any other value INCLUDING AN ARRAY replaces the stored value (to change one sale/building/tax district, send the WHOLE array); null DELETES the field; omitted fields are unchanged. " +
		"Cannot change folio_number, property_address or ingested_at, or server fields (PK, SK, hashes, revision, api_*). Strict schema: numbers must be JSON numbers (not strings), unknown keys are rejected — put anything else under 'additional_details' (free-form). " +
		"Typed fields: legal_description, lat, lon, data_version ('MM.DD.YYYY'), mailing_address {line1, line2, city, state, zip, country, formatted_address}, owners [{name}], identification {folio, primary_id, primary_id_type, pin, strap, subdivision_id, municipality_code, neighborhood_code, property_use_code, is_inactive, alt_ids}, " +
		"valuation {market_value, assessed_value, taxable_value, land_value, building_value, tax_authorities_breakdown [{authority_name, authority_type, millage_rate, tax_amount}]}, buildings [{bedrooms, bathrooms, year_built, effective_year_built, heated_sqft, gross_sqft, total_stories, total_units, addresses, sub_areas, structural_elements}], " +
		"sales [{date 'YYYY-MM-DD', price, grantor, grantee, book_page, instrument_number, instrument_type, qualified, reason_code, vacant_improved}], extras {acreage, total_buildings, county_last_edit_date}, land [..], permits [..], additional_site_addresses [..], source {county, state}. Arrays max 500 items. " +
		"tax_authorities_breakdown is what fills the Taxes tab's annual tax, millage and district cards. " +
		"Returns { pk, folio, revision (the NEW one — use it for the next update), fields { updated, removed }, record }; revision rises even if nothing changed. " +
		"Errors: PRECONDITION_FAILED (412) → someone changed it: get_parcel again, reconcile, then update with the new revision — NEVER resend the same patch with a newer revision; VALIDATION → fix every field in details.errors; PARCEL_NOT_FOUND → wrong pk/folio."}, h.UpdateParcel)

	mcp.AddTool(server, &mcp.Tool{Name: "create_parcel", Description: "Create " + parcelWhat + " ONLY after get_parcel returned PARCEL_NOT_FOUND for the address and the user confirmed the data — records can't be deleted. Requires 'parcel', the full record: folio_number (max 64 chars, no '#'), property_address {line1, city, state (2 capital letters), zip (5-digit or ZIP+4), country, formatted_address}, source {county, state}; " +
		"optional: the same typed fields as update_parcel (valuation, buildings, sales, owners, identification, extras, mailing_address, legal_description, lat, lon, land, permits, additional_site_addresses, additional_details), data_version ('MM.DD.YYYY', defaults to today), ingested_at (RFC 3339, defaults to now). " +
		"Do NOT send PK, SK, hashes, revision or api_* — the server sets them. Strict schema: numbers as JSON numbers, unknown keys rejected (use additional_details). " +
		"Returns { pk, sk, folio, revision: 1, record }. Errors: PARCEL_EXISTS → NOTHING written; call get_parcel and use update_parcel instead; VALIDATION → fix every field in details.errors. Not safe to retry blindly after a timeout — call get_parcel first."}, h.CreateParcel)
}
