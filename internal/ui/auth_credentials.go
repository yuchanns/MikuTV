package tvui

import (
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	credServerField   app.FocusID = "auth/cred/server"
	credServerEdit    app.FocusID = "auth/cred/server-edit"
	credUsernameField app.FocusID = "auth/cred/username"
	credUsernameEdit  app.FocusID = "auth/cred/username-edit"
	credPasswordField app.FocusID = "auth/cred/password"
	credPasswordEdit  app.FocusID = "auth/cred/password-edit"
	credSubmitBtn     app.FocusID = "auth/cred/submit"

	credServerMaxLen   = 256
	credUsernameMaxLen = 128
	credPasswordMaxLen = 256
)

type credentialsField uint8

const (
	credFieldNone credentialsField = iota
	credFieldServer
	credFieldUsername
	credFieldPassword
)

type credentialsDraft struct {
	serverURL  string
	username   string
	password   string
	input      string
	editing    credentialsField
	localError string
}

func (d *credentialsDraft) clear() {
	d.serverURL = ""
	d.username = ""
	d.password = ""
	d.input = ""
	d.editing = credFieldNone
	d.localError = ""
}

func (d *credentialsDraft) fieldFocus() app.FocusID {
	switch d.editing {
	case credFieldServer:
		return credServerField
	case credFieldPassword:
		return credPasswordField
	default:
		return credUsernameField
	}
}

type credentialsSignIn struct{}

func (credentialsSignIn) Render(c *ui.Context, p *PageContext) {
	v := p.host
	draft := &v.credDraft
	snapshot, labels, colors := v.Auth.SignIn(), p.Assets.Text.Auth, p.Theme.Colors
	entry, _ := p.State.Current()

	if snapshot.Phase == app.SignInSuccess {
		ui.Column(c.Key("auth-cred-success")).FillWidth().MinHeight(360).Center().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
			p.authProgress(c, "check", labels.Success, labels.Continuing)
		})
		return
	}

	title := entry.Auth.CredentialsTitle
	if title == "" {
		title = labels.CredentialsTitle
	}
	description := entry.Auth.CredentialsDescription
	if description == "" {
		description = labels.CredentialsBody
	}
	serverPlaceholder := entry.Auth.ServerURLPlaceholder
	if serverPlaceholder == "" {
		serverPlaceholder = labels.ServerURLPlaceholder
	}
	usernamePlaceholder := entry.Auth.UsernamePlaceholder
	if usernamePlaceholder == "" {
		usernamePlaceholder = labels.UsernamePlaceholder
	}

	usesServer := entry.UsesServerURL()
	busy := snapshot.Phase == app.SignInLoading

	if usesServer {
		p.region("cred-server", focusEdges{app.Down: {Focus: credUsernameField}})
		p.region("cred-username", focusEdges{app.Up: {Focus: credServerField}, app.Down: {Focus: credPasswordField}})
	} else {
		p.region("cred-username", focusEdges{app.Down: {Focus: credPasswordField}})
	}
	p.region("cred-password", focusEdges{app.Up: {Focus: credUsernameField}, app.Down: {Focus: credSubmitBtn}})
	p.region("cred-actions", focusEdges{app.Up: {Focus: credPasswordField}})

	ui.Column(c.Key("auth-credentials")).FillWidth().Lang(p.langTag()).Font(p.uiFont()).Children(func() {
		p.framelessHeader(c, "ACCOUNT \u00b7 SIGN IN", "user", title, description)

		if usesServer {
			p.renderCredentialsField(c, credentialsFieldSpec{
				kind:        credFieldServer,
				region:      "cred-server",
				fieldFocus:  credServerField,
				editFocus:   credServerEdit,
				nextFocus:   credUsernameField,
				label:       labels.ServerURLLabel,
				placeholder: serverPlaceholder,
				value:       draft.serverURL,
				maxLen:      credServerMaxLen,
				keyboard:    ui.KeyboardURL,
				returnKey:   ui.ReturnNext,
				autofill:    ui.AutofillNone,
				busy:        busy,
				onCommit: func(val string) {
					draft.serverURL = strings.TrimSpace(val)
				},
			})
		}

		p.renderCredentialsField(c, credentialsFieldSpec{
			kind:        credFieldUsername,
			region:      "cred-username",
			fieldFocus:  credUsernameField,
			editFocus:   credUsernameEdit,
			nextFocus:   credPasswordField,
			label:       labels.UsernameLabel,
			placeholder: usernamePlaceholder,
			value:       draft.username,
			maxLen:      credUsernameMaxLen,
			keyboard:    ui.KeyboardDefault,
			returnKey:   ui.ReturnNext,
			autofill:    ui.AutofillUsername,
			busy:        busy,
			onCommit: func(val string) {
				draft.username = strings.TrimSpace(val)
			},
		})

		p.renderCredentialsField(c, credentialsFieldSpec{
			kind:        credFieldPassword,
			region:      "cred-password",
			fieldFocus:  credPasswordField,
			editFocus:   credPasswordEdit,
			nextFocus:   credSubmitBtn,
			label:       labels.PasswordLabel,
			placeholder: labels.PasswordPlaceholder,
			value:       draft.password,
			maxLen:      credPasswordMaxLen,
			keyboard:    ui.KeyboardDefault,
			returnKey:   ui.ReturnDone,
			autofill:    ui.AutofillPassword,
			password:    true,
			busy:        busy,
			onCommit: func(val string) {
				draft.password = val
			},
		})

		errText := draft.localError
		if errText == "" && snapshot.Phase == app.SignInFailed {
			errText = p.credentialsAuthError(snapshot.Error)
		}
		if errText != "" {
			ui.Text(c.Key("auth-cred-error"), errText).FillWidth().Margin(18, 0, 0, 0).
				FontSize(18).LineHeight(1.5).TextColor(paint(colors.Status.Error.Foreground))
		}

		submitLabel := labels.SubmitCredentials
		if busy {
			submitLabel = labels.SubmittingCredentials
		}
		ui.Row(c.Key("auth-cred-actions")).Margin(32, 0, 0, 0).Gap(16).Children(func() {
			p.styledButton(c, submitLabel, "", credSubmitBtn, "cred-actions", func() {
				v.submitCredentials()
			}, p.framelessButtonAppearance())
			p.styledButton(c, p.Assets.Text.Cancel, "", app.CloseModal, "cred-actions", func() {
				v.Auth.CancelSignIn()
				p.State.Back()
			}, p.framelessButtonAppearance())
		})
	})
}

type credentialsFieldSpec struct {
	kind        credentialsField
	region      string
	fieldFocus  app.FocusID
	editFocus   app.FocusID
	nextFocus   app.FocusID
	label       string
	placeholder string
	value       string
	maxLen      int
	keyboard    ui.KeyboardType
	returnKey   ui.ReturnKeyType
	autofill    ui.AutofillKind
	password    bool
	busy        bool
	onCommit    func(string)
}

func (p *PageContext) renderCredentialsField(c *ui.Context, spec credentialsFieldSpec) {
	v := p.host
	draft := &v.credDraft
	colors, d := p.Theme.Colors, p.uiColors()

	ui.Column(c.Key("cred-group-" + spec.region)).FillWidth().Margin(20, 0, 0, 0).Gap(8).Children(func() {
		ui.Text(c, spec.label).FontSize(16).FontWeight(500).TextColor(paint(colors.Text.Secondary))
		if draft.editing == spec.kind {
			ui.Row(c.Key(spec.region + "-edit-box")).FillWidth().Height(60).Padding(0, 22).
				AlignItems(ui.Center).Radius(12).Background(paint(colors.Surface.Input)).
				Border(1, paint(colors.Focus.Ring)).Shadow(0, 0, 0, 3, paint(colors.Focus.Ring)).Children(func() {
				field := ui.TextInput(c.Key(spec.region+"-input"), &draft.input).Grow(1).MinWidth(0).Height(42).
					Padding(0, 0).Border(0, ui.Transparent).Background(ui.Transparent).
					FontSize(22).TextColor(paint(colors.Text.Primary)).
					Label(spec.label).Placeholder(spec.placeholder).PlaceholderColor(paint(colors.Text.Muted)).
					MaxLength(spec.maxLen).Keyboard(spec.keyboard).ReturnKey(spec.returnKey).
					Capitalize(ui.CapitalizeNone).Autocorrect(false)
				if spec.autofill != ui.AutofillNone {
					field.Autofill(spec.autofill)
				}
				if spec.password {
					field.Password()
				}
				field.OnChange(func() {
					spec.onCommit(draft.input)
					draft.localError = ""
				}).OnTextKey(func(key ui.TextKey) bool {
					switch key.Key {
					case ui.KeyEnter:
						spec.onCommit(key.Text())
						draft.input = ""
						draft.editing = credFieldNone
						draft.localError = ""
						v.State.Focus, v.reveal = spec.nextFocus, true
						return true
					case ui.KeyEscape:
						draft.input = ""
						draft.editing = credFieldNone
						v.State.Focus, v.reveal = spec.fieldFocus, true
						return true
					default:
						return false
					}
				})
				if p.active {
					field.FocusRing(false)
					field.FocusBind(&p.State.Focus, spec.editFocus)
					field.Focus(ui.FocusOptions{PreventScroll: true})
				}
			})
			return
		}

		display := spec.value
		ink := paint(colors.Text.Primary)
		if spec.password && display != "" {
			display = strings.Repeat("\u2022", utf8.RuneCountInString(display))
		}
		if display == "" {
			display = spec.placeholder
			ink = paint(colors.Text.Muted)
		}
		accessible := spec.label
		if !spec.password && spec.value != "" {
			accessible = spec.label + ": " + spec.value
		}
		focused := p.paintFocus() == spec.fieldFocus
		border := d.strongBorder
		if focused {
			border = paint(colors.Focus.Ring)
		}
		btn := ui.Button(c.Key(string(spec.fieldFocus)), "").Label(accessible).
			FillWidth().Height(60).Padding(0, 22).Radius(12).
			Background(paint(colors.Surface.Input)).Border(1, border).TextColor(ink).
			OnClick(func() {
				p.dispatch(func() {
					if spec.busy {
						return
					}
					draft.input = spec.value
					draft.editing = spec.kind
					draft.localError = ""
					p.State.Focus = spec.editFocus
				})
			})
		if focused {
			btn.Shadow(0, 0, 0, 3, paint(colors.Focus.Ring))
		}
		p.bind(btn, spec.fieldFocus, spec.region)
		btn.Children(func() {
			ui.Row(c).Fill().AlignItems(ui.Center).Children(func() {
				ui.Text(c, display).Grow(1).MinWidth(0).SingleLine().FontSize(22).TextColor(ink)
			})
		})
	})
}

func (v *View) submitCredentials() {
	if v.Auth == nil {
		return
	}
	if phase := v.Auth.SignIn().Phase; phase == app.SignInLoading || phase == app.SignInSuccess {
		return
	}
	entry, ok := v.State.Current()
	if !ok {
		return
	}
	draft := &v.credDraft
	labels := v.Assets.Text.Auth
	serverURL := strings.TrimSpace(draft.serverURL)
	username := strings.TrimSpace(draft.username)
	draft.serverURL = serverURL
	draft.username = username

	if entry.RequiresServerURL() || (entry.UsesServerURL() && serverURL != "") {
		_, normalized, err := plugin.NormalizeServerOrigin(serverURL)
		if err != nil {
			draft.localError = labels.ServerURLInvalid
			v.State.Focus, v.reveal = credServerField, true
			return
		}
		serverURL = normalized
		draft.serverURL = normalized
	}
	if username == "" {
		draft.localError = labels.UsernameRequired
		v.State.Focus, v.reveal = credUsernameField, true
		return
	}

	draft.localError = ""
	draft.editing = credFieldNone
	draft.input = ""
	v.Auth.StartCredentialsSignIn(provider.CredentialsRequest{
		ServerURL: serverURL,
		Username:  username,
		Password:  draft.password,
	}, v.dismissAfter(app.SignInCredentials))
}

func (v *View) backFromCredentialsSignIn() bool {
	if v.credDraft.editing != credFieldNone {
		target := v.credDraft.fieldFocus()
		v.credDraft.editing = credFieldNone
		v.credDraft.input = ""
		v.State.Focus, v.reveal = target, true
		return true
	}
	return v.Auth != nil && v.Auth.SignIn().Phase == app.SignInSuccess
}

func (p *PageContext) credentialsAuthError(code provider.ErrorCode) string {
	switch code {
	case provider.NeedsSignIn, provider.Denied:
		return p.Assets.Text.Auth.InvalidCredentials
	case provider.Network, provider.Timeout, provider.RateLimited:
		return p.Assets.Text.Auth.NetworkFailed
	default:
		return p.Assets.Text.Auth.Failed
	}
}
