package ichorgo

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	// Far more than an answer that fits a phone: a server streaming without end is stopped.
	maxAnswerBytes = 256 << 10
	// The listener gets the whole answer each time, so it is not called for every token.
	answerUpdateEvery = 80 * time.Millisecond

	reportOutOfDate = "the report is out of date: refresh it"
)

// DiagnosisListener receives the model's answer while it is written (implemented in
// Kotlin/Swift).
type DiagnosisListener interface {
	// OnAnswer gets the whole answer so far, not only what is new.
	OnAnswer(text string)
	// OnDone is called exactly once; errMessage is empty on success and when cancelled.
	OnDone(errMessage string)
}

// DiagnosisRun is a handle on a question being answered.
type DiagnosisRun struct {
	cancel context.CancelFunc
}

// Cancel stops waiting for the answer; OnDone is still called.
func (r *DiagnosisRun) Cancel() { r.cancel() }

// Diagnosis is a report about the cluster, ready to be shown to the user and, if they
// decide so, sent to a model. It lives in memory only.
type Diagnosis struct {
	report string
	// mask is what replaced names and addresses in report, nil when it holds the real ones.
	mask *privacyMask
	// ownMask tells that mask belongs to this diagnosis rather than to screenshot mode.
	ownMask bool
	// Screenshot mode when the report was collected: once it changes, the report shows the
	// wrong names (real ones in screenshot mode, or fakes nobody can map back).
	screenshotMode   bool
	screenshotResets int
}

// CollectDiagnosis reads the state of the cluster (os:reader calls only) into a report.
// Nothing leaves the phone here. With anonymize, the nodes' names, addresses and domain
// are replaced with placeholders, and answers get the real ones back before they are
// shown; in screenshot mode the report is masked like everything else on screen.
func CollectDiagnosis(configYAML, contextName string, anonymize bool) (d *Diagnosis, err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)

	d = &Diagnosis{}
	d.screenshotMode, d.screenshotResets = privacy.state()

	data, err := withSession(configYAML, contextName, diagnosisCollectTimeout, func(ctx context.Context, s *session) (diagnosisData, error) {
		return collectDiagnosis(ctx, s), nil
	})
	if err != nil {
		return nil, err
	}

	d.report = renderDiagnosis(data)

	switch {
	case d.screenshotMode:
		d.mask = privacy
	case anonymize:
		d.mask, d.ownMask = &privacyMask{}, true
		d.mask.set(true, nil)
		// Placeholders that mean nothing else in the report: "worker-1" may be a pod, and
		// the real network may be the one the placeholder addresses come from.
		d.mask.setAvoid(d.report)
		d.mask.learnConfig(configYAML)
	}

	if d.mask != nil {
		learnDiagnosisHosts(d.mask, data)
		d.report = d.mask.maskPlain(d.report)
	}

	return d, nil
}

// Report is the text that would be sent, to show to the user first.
func (d *Diagnosis) Report() string { return d.report }

// Anonymized tells whether names and addresses in the report are placeholders.
func (d *Diagnosis) Anonymized() bool { return d.mask != nil }

// Prompt is the whole question as one text (instructions, report and the user's note), to
// hand to another app such as an assistant the user already has. language is the app's
// language tag ("fr"). It is empty when the report is out of date and must be collected again.
func (d *Diagnosis) Prompt(language, note string) string {
	if d.outOfDate() {
		return ""
	}

	return diagnosisSystemPrompt(language, d.mask != nil) + "\n\n" + diagnosisUserMessage(d.report, d.maskNote(note))
}

// Ask sends the report and the user's optional note to the model and streams its answer
// to listener. provider is "anthropic" or "openai"; an empty model means the provider's
// default, an empty baseURL its own API.
func (d *Diagnosis) Ask(provider, apiKey, model, baseURL, language, note string, listener DiagnosisListener) *DiagnosisRun {
	listener = maskedDiagnosisListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), aiAnswerTimeout)

	go func() {
		defer cancel()

		listener.OnDone(strings.ToValidUTF8(d.ask(ctx, provider, apiKey, model, baseURL, language, note, listener), "�"))
	}()

	return &DiagnosisRun{cancel: cancel}
}

func (d *Diagnosis) ask(ctx context.Context, provider, apiKey, model, baseURL, language, note string, listener DiagnosisListener) string {
	if d.outOfDate() {
		return reportOutOfDate
	}

	req, err := newAIRequest(provider, apiKey, model, baseURL)
	if err != nil {
		return err.Error()
	}

	req.system = diagnosisSystemPrompt(language, d.mask != nil)
	req.user = diagnosisUserMessage(d.report, d.maskNote(note))

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	var (
		answer  strings.Builder
		shown   int
		shownAt time.Time
		tooLong bool
	)

	show := func() {
		listener.OnAnswer(d.reveal(answer.String()))
		shown, shownAt = answer.Len(), time.Now()
	}

	err = streamAnswer(ctx, req, func(piece string) {
		if tooLong {
			return
		}

		answer.WriteString(piece)

		if answer.Len() > maxAnswerBytes {
			tooLong = true

			stop()

			return
		}

		if time.Since(shownAt) >= answerUpdateEvery {
			show()
		}
	})

	if answer.Len() != shown {
		show()
	}

	switch {
	case tooLong:
		return "the answer is far too long, it was stopped"
	case errors.Is(err, context.Canceled):
		return ""
	case err != nil:
		return err.Error()
	case strings.TrimSpace(answer.String()) == "":
		return "the model returned an empty answer"
	default:
		return ""
	}
}

// outOfDate tells that screenshot mode changed since the report was collected.
func (d *Diagnosis) outOfDate() bool {
	on, resets := privacy.state()

	return on != d.screenshotMode || on && resets != d.screenshotResets
}

// maskNote hides in the user's note what the report hides. The user may write the real
// names or the placeholders they read in the report: placeholders are first turned back
// into what they stand for, so both end up as the report's placeholders.
func (d *Diagnosis) maskNote(note string) string {
	if d.mask == nil {
		return note
	}

	return d.mask.maskPlain(d.mask.reveal(note))
}

// reveal puts the real names and addresses back in an answer written about placeholders.
// (In screenshot mode the listener then masks them again, like everything displayed.)
func (d *Diagnosis) reveal(answer string) string {
	if d.mask == nil {
		return answer
	}

	return d.mask.reveal(answer)
}
