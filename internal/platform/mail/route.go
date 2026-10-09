package mail

import "context"

// Route returns a Sender that delivers the messages whose recipient
// matches through matched and every other message through rest. Staging
// uses it to send the automated suite's test users (see package
// testusers) to a Mailpit the suite can read, while real recipients
// keep getting Mailjet.
func Route(match func(email string) bool, matched, rest Sender) Sender {
	return &router{match: match, matched: matched, rest: rest}
}

type router struct {
	match         func(email string) bool
	matched, rest Sender
}

func (r *router) Send(ctx context.Context, msg Message) error {
	if r.match(msg.ToEmail) {
		return r.matched.Send(ctx, msg)
	}

	return r.rest.Send(ctx, msg)
}
