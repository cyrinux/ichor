package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
)

// After an IAM Identity Center sign-in with only the start URL and region, the portal lists
// the accounts and roles the user may assume (like `aws configure sso`): one is taken as is,
// several are offered to the user, who picks one on the sign-in sheet.

// awsSSORole is an account and a role the signed-in user may assume.
type awsSSORole struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	RoleName    string `json:"roleName"`
}

const (
	// awsSSOMaxAccounts caps the accounts listed (one roles call each).
	awsSSOMaxAccounts = 50
	// awsSSOMaxPages caps the pages of one listing.
	awsSSOMaxPages = 20
	// ssoRolesSession is the session key of the role list waiting for the user's choice.
	ssoRolesSession = "ssoRolesJson"
)

// ssoRoles lists the accounts and roles the token may assume, sorted by account name then
// role.
func (m *eksMethod) ssoRoles(ctx context.Context, token, region string) ([]awsSSORole, error) {
	type account struct {
		AccountID   string `json:"accountId"`
		AccountName string `json:"accountName"`
	}

	var accounts []account

	err := m.portalPages(ctx, token, region, "/assignment/accounts", url.Values{}, func(data json.RawMessage) error {
		var page struct {
			AccountList []account `json:"accountList"`
		}

		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}

		accounts = append(accounts, page.AccountList...)

		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(accounts) > awsSSOMaxAccounts {
		accounts = accounts[:awsSSOMaxAccounts]
	}

	var roles []awsSSORole

	for _, acc := range accounts {
		err := m.portalPages(ctx, token, region, "/assignment/roles", url.Values{"account_id": {acc.AccountID}}, func(data json.RawMessage) error {
			var page struct {
				RoleList []struct {
					RoleName string `json:"roleName"`
				} `json:"roleList"`
			}

			if err := json.Unmarshal(data, &page); err != nil {
				return err
			}

			for _, r := range page.RoleList {
				roles = append(roles, awsSSORole{AccountID: acc.AccountID, AccountName: acc.AccountName, RoleName: r.RoleName})
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	slices.SortFunc(roles, func(a, b awsSSORole) int {
		return cmp.Or(cmp.Compare(a.AccountName, b.AccountName), cmp.Compare(a.AccountID, b.AccountID), cmp.Compare(a.RoleName, b.RoleName))
	})

	return roles, nil
}

// portalPages GETs every page of a portal listing, handing each answer to read.
func (m *eksMethod) portalPages(ctx context.Context, token, region, path string, query url.Values, read func(json.RawMessage) error) error {
	next := ""

	for range awsSSOMaxPages {
		q := maps.Clone(query)
		q.Set("max_result", "100")

		if next != "" {
			q.Set("next_token", next)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, awsSSOPortalEndpoint(region)+path+"?"+q.Encode(), nil)
		if err != nil {
			return err
		}

		req.Header.Set("X-Amz-Sso_bearer_token", token)

		var raw json.RawMessage

		status, err := m.doJSON(req, &raw)
		if err != nil {
			return err
		}

		var page struct {
			NextToken string `json:"nextToken"`
			Message   string `json:"message"`
		}

		_ = json.Unmarshal(raw, &page)

		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return signInRequired(authEKS, page.Message)
		}

		if status != http.StatusOK {
			return fmt.Errorf("IAM Identity Center could not list the accounts: %s", cmpOr(page.Message, strconv.Itoa(status)))
		}

		if err := read(raw); err != nil {
			return fmt.Errorf("IAM Identity Center accounts: %w", err)
		}

		if next = page.NextToken; next == "" {
			return nil
		}
	}

	return nil
}

// withChosenRole is state with the role picked: one of the list, or the only one.
func withChosenRole(state kubeAuthState, role awsSSORole) kubeAuthState {
	out := state
	out.Secrets = maps.Clone(state.Secrets)

	if out.Secrets == nil {
		out.Secrets = map[string]string{}
	}

	out.Secrets[awsFieldAccountID], out.Secrets[awsFieldRoleName] = role.AccountID, role.RoleName
	out.User = ssoRoleLabel(role)

	return out
}

// ssoRoleLabel is who the sign-in is: "<role> @ <account name> (<id>)", or "<role> @ <id>".
func ssoRoleLabel(role awsSSORole) string {
	if role.AccountName == "" {
		return role.RoleName + " @ " + role.AccountID
	}

	return role.RoleName + " @ " + role.AccountName + " (" + role.AccountID + ")"
}

// ssoNeedsRole tells a signed-in Identity Center state that has no account and role yet.
func ssoNeedsRole(state kubeAuthState) bool {
	return state.secret(awsFieldSSOStartURL) != "" && (state.secret(awsFieldAccountID) == "" || state.secret(awsFieldRoleName) == "") &&
		(state.session("ssoAccess") != "" || state.session("ssoRefresh") != "")
}

// ssoChoices is the role list a signed-in state waits on the user to pick from.
func ssoChoices(state kubeAuthState) []awsSSORole {
	if !ssoNeedsRole(state) {
		return nil
	}

	var roles []awsSSORole
	if json.Unmarshal([]byte(state.session(ssoRolesSession)), &roles) != nil {
		return nil
	}

	return roles
}

// withSignedInSession is entered (the four fields, the role picked) with the session of the
// stored sign-in that was waiting for that choice, so picking needs no second sign-in. Any
// other entry starts afresh.
func withSignedInSession(stored, entered kubeAuthState) kubeAuthState {
	if !ssoNeedsRole(stored) || entered.secret(awsFieldAccountID) == "" || entered.secret(awsFieldRoleName) == "" ||
		stored.secret(awsFieldSSOStartURL) != entered.secret(awsFieldSSOStartURL) || stored.secret(awsFieldSSORegion) != entered.secret(awsFieldSSORegion) {
		return entered
	}

	chosen := awsSSORole{AccountID: entered.secret(awsFieldAccountID), RoleName: entered.secret(awsFieldRoleName)}

	for _, r := range ssoChoices(stored) {
		if r.AccountID == chosen.AccountID && r.RoleName == chosen.RoleName {
			chosen = r
		}
	}

	out := withChosenRole(entered, chosen)
	out.Session = stored.Session
	out.SessionExpires = stored.SessionExpires

	return out.withSession(map[string]string{ssoRolesSession: ""})
}
