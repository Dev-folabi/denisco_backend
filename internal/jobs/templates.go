// Package jobs defines the background tasks the worker runs and the email
// bodies they produce.
package jobs

import (
	"fmt"
	"html"
	"strings"

	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/mail"
)

// brand colours, taken from the design system so email matches the site.
const (
	colourForest = "#163A1F"
	colourOlive  = "#4E7A38"
	colourCream  = "#FAF6EC"
	colourLine   = "#E4DCC8"
	colourMuted  = "#6B6456"
)

// Money renders an amount in kobo as naira, the way the site does.
func Money(kobo int64) string {
	naira := kobo / 100
	remainder := kobo % 100

	// Thousands separators, matching the site's en-NG formatting.
	digits := fmt.Sprintf("%d", naira)
	var grouped strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}

	if remainder == 0 {
		return "₦" + grouped.String()
	}
	return fmt.Sprintf("₦%s.%02d", grouped.String(), remainder)
}

// email is a rendered message plus the address it goes to.
type email struct {
	to      string
	subject string
	html    string
	text    string
}

// toMessage converts a rendered email into a provider message.
func (e email) toMessage() mail.Message {
	return mail.Message{To: e.to, Subject: e.subject, HTML: e.html, Text: e.text}
}

// str reads a string from an event payload.
func str(data map[string]any, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

// money reads an amount from an event payload. JSON numbers decode as float64
// through the queue, so both shapes are accepted.
func money(data map[string]any, key string) int64 {
	switch value := data[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int32:
		return int64(value)
	case int:
		return int64(value)
	default:
		return 0
	}
}

// Render turns an outbox event into the customer's copy of the news. It
// returns false for events the customer hears nothing about — staff-only
// notices and the farm's own view of an event live in RenderAdmin.
func Render(eventType string, data map[string]any, siteURL string) (email, bool) {
	siteURL = strings.TrimRight(siteURL, "/")

	switch eventType {
	case outbox.TypeUserRegistered:
		name := str(data, "first_name")
		return email{
			to:      str(data, "email"),
			subject: "Welcome to DENISCO Global Agriculture",
			html: layout("Welcome to DENISCO", greeting(name),
				`<p>Your account is ready. You can now order fresh farm produce and book a consultation with our agricultural experts.</p>`,
				button(siteURL+"/shop", "Start Shopping")),
			text: fmt.Sprintf("Welcome to DENISCO Global Agriculture.\n\nYour account is ready. Visit %s/shop to start shopping.", siteURL),
		}, true

	case outbox.TypePasswordResetRequested:
		resetURL := str(data, "reset_url")
		return email{
			to:      str(data, "email"),
			subject: "Reset your DENISCO password",
			html: layout("Reset your password", greeting(str(data, "first_name")),
				`<p>We received a request to reset your password. This link expires in one hour and can be used once.</p>`+
					`<p style="color:`+colourMuted+`;font-size:13px;">If you did not ask for this, you can safely ignore this email — your password will not change.</p>`,
				button(resetURL, "Reset Password")),
			text: fmt.Sprintf("Reset your DENISCO password using this link (valid for one hour):\n\n%s\n\nIf you did not ask for this, ignore this email.", resetURL),
		}, true

	case outbox.TypePasswordChanged:
		return email{
			to:      str(data, "email"),
			subject: "Your DENISCO password was changed",
			html: layout("Password changed", "",
				`<p>Your password has just been changed and every signed-in session was ended.</p>`+
					`<p style="color:`+colourMuted+`;font-size:13px;">If this was not you, contact us immediately.</p>`, ""),
			text: "Your DENISCO password was changed and all sessions were signed out. If this was not you, contact us immediately.",
		}, true

	case outbox.TypeOrderCreated:
		number := str(data, "order_number")
		return email{
			to:      str(data, "email"),
			subject: "Your DENISCO order " + number,
			html: layout("Order received", greeting(str(data, "name")),
				fmt.Sprintf(`<p>We have your order <strong>%s</strong> for <strong>%s</strong>.</p>
					<p>The items are held for you until payment is confirmed.</p>`,
					html.EscapeString(number), Money(money(data, "total"))),
				button(siteURL+"/account/orders", "View Order")),
			text: fmt.Sprintf("We have your order %s for %s. The items are held until payment is confirmed.", number, Money(money(data, "total"))),
		}, true

	case outbox.TypeOrderPaid:
		number := str(data, "order_number")
		return email{
			to:      str(data, "email"),
			subject: "Payment confirmed for order " + number,
			html: layout("Payment confirmed", greeting(str(data, "name")),
				fmt.Sprintf(`<p>Your payment of <strong>%s</strong> for order <strong>%s</strong> has been confirmed.</p>
					<p>We are preparing your order and will let you know when it is on its way.</p>`,
					Money(money(data, "total")), html.EscapeString(number)),
				button(siteURL+"/account/orders", "Track Order")),
			text: fmt.Sprintf("Payment of %s for order %s confirmed. We are preparing your order.", Money(money(data, "total")), number),
		}, true

	case outbox.TypeOrderStatusChanged:
		number := str(data, "order_number")
		status := str(data, "status")
		return email{
			to:      str(data, "email"),
			subject: fmt.Sprintf("Order %s is now %s", number, status),
			html: layout("Order update", greeting(str(data, "name")),
				fmt.Sprintf(`<p>Order <strong>%s</strong> is now <strong>%s</strong>.</p>`,
					html.EscapeString(number), html.EscapeString(status)),
				button(siteURL+"/account/orders", "View Order")),
			text: fmt.Sprintf("Order %s is now %s.", number, status),
		}, true

	case outbox.TypeOrderCancelled:
		number := str(data, "order_number")
		return email{
			to:      str(data, "email"),
			subject: "Order " + number + " cancelled",
			html: layout("Order cancelled", greeting(str(data, "name")),
				fmt.Sprintf(`<p>Order <strong>%s</strong> has been cancelled and the items returned to the shop.</p>`,
					html.EscapeString(number)),
				button(siteURL+"/shop", "Continue Shopping")),
			text: fmt.Sprintf("Order %s has been cancelled and the items returned to the shop.", number),
		}, true

	case outbox.TypeOrderRefunded:
		number := str(data, "order_number")
		return email{
			to:      str(data, "email"),
			subject: "Refund issued for order " + number,
			html: layout("Refund issued", greeting(str(data, "name")),
				fmt.Sprintf(`<p>We have refunded <strong>%s</strong> for order <strong>%s</strong>.</p>
					<p>The money returns to the payment method you used, which usually takes 3–5 business days to appear.</p>`,
					Money(money(data, "total")), html.EscapeString(number)),
				button(siteURL+"/account/orders", "View Order")),
			text: fmt.Sprintf("We have refunded %s for order %s. The money returns to the payment method you used, which usually takes 3-5 business days.", Money(money(data, "total")), number),
		}, true

	case outbox.TypeOrderPaymentFailed:
		number := str(data, "order_number")
		return email{
			to:      str(data, "email"),
			subject: "Payment did not go through — order " + number,
			html: layout("Payment not completed", greeting(str(data, "name")),
				fmt.Sprintf(`<p>We could not take payment of <strong>%s</strong> for order <strong>%s</strong>. No money has left your account.</p>
					<p>The items have been returned to the shop, so you will need to place the order again — prices and availability may have changed since.</p>`,
					Money(money(data, "total")), html.EscapeString(number)),
				button(siteURL+"/account/orders", "View Order")),
			text: fmt.Sprintf("We could not take payment of %s for order %s. No money has left your account. The items have been returned to the shop, so you will need to place the order again.", Money(money(data, "total")), number),
		}, true

	case outbox.TypeConsultationBooked:
		details := map[string]string{
			"Reference": str(data, "reference"),
			"Date":      str(data, "date"),
			"Time":      str(data, "time"),
		}

		// A consultation fee is paid through Paystack, so the confirmation
		// says where the booking stands on it rather than leaving the
		// customer to guess.
		closing := "We will confirm your appointment shortly."
		if fee := money(data, "amount"); fee > 0 {
			details["Fee"] = Money(fee)
			if str(data, "payment_status") == "paid" {
				closing = "Your fee is paid. We will confirm your appointment shortly."
			} else {
				closing = "Your appointment is confirmed once the fee is paid."
			}
		}

		return email{
			to:      str(data, "email"),
			subject: "Your consultation is booked — " + str(data, "reference"),
			html: layout("Consultation booked", greeting(str(data, "name")),
				fmt.Sprintf(`<p>Your <strong>%s</strong> is booked.</p>
					%s
					<p style="color:`+colourMuted+`;font-size:13px;">%s</p>`,
					html.EscapeString(str(data, "type_name")),
					detailBox(details),
					html.EscapeString(closing)),
				button(siteURL+"/account/consultations", "View Booking")),
			text: fmt.Sprintf("Your %s is booked.\nReference: %s\nDate: %s\nTime: %s\n%s",
				str(data, "type_name"), str(data, "reference"), str(data, "date"), str(data, "time"), closing),
		}, true

	case outbox.TypeConsultationCancelled:
		return email{
			to:      str(data, "email"),
			subject: "Consultation " + str(data, "reference") + " cancelled",
			html: layout("Consultation cancelled", greeting(str(data, "name")),
				fmt.Sprintf(`<p>Your <strong>%s</strong> on %s at %s has been cancelled.</p>`,
					html.EscapeString(str(data, "type_name")),
					html.EscapeString(str(data, "date")),
					html.EscapeString(str(data, "time"))),
				button(siteURL+"/consultation", "Book Again")),
			text: fmt.Sprintf("Your %s on %s at %s has been cancelled.",
				str(data, "type_name"), str(data, "date"), str(data, "time")),
		}, true

	case outbox.TypeConsultationStatus:
		return email{
			to:      str(data, "email"),
			subject: fmt.Sprintf("Consultation %s is now %s", str(data, "reference"), str(data, "status")),
			html: layout("Consultation update", greeting(str(data, "name")),
				fmt.Sprintf(`<p>Your <strong>%s</strong> on %s at %s is now <strong>%s</strong>.</p>`,
					html.EscapeString(str(data, "type_name")),
					html.EscapeString(str(data, "date")),
					html.EscapeString(str(data, "time")),
					html.EscapeString(str(data, "status"))),
				button(siteURL+"/account/consultations", "View Booking")),
			text: fmt.Sprintf("Your %s on %s at %s is now %s.",
				str(data, "type_name"), str(data, "date"), str(data, "time"), str(data, "status")),
		}, true

	case outbox.TypeConsultationRefunded:
		reference := str(data, "reference")
		return email{
			to:      str(data, "email"),
			subject: "Consultation fee refunded — " + reference,
			html: layout("Fee refunded", greeting(str(data, "name")),
				fmt.Sprintf(`<p>We have refunded your fee of <strong>%s</strong> for the <strong>%s</strong> booked under <strong>%s</strong>.</p>
					<p>The money returns to the payment method you used, which usually takes 3–5 business days to appear.</p>`,
					Money(money(data, "amount")),
					html.EscapeString(str(data, "type_name")),
					html.EscapeString(reference)),
				button(siteURL+"/account/consultations", "View Bookings")),
			text: fmt.Sprintf("We have refunded your fee of %s for the %s booked under %s. The money returns to the payment method you used, which usually takes 3-5 business days.",
				Money(money(data, "amount")), str(data, "type_name"), reference),
		}, true

	case outbox.TypeConsultationPaymentFailed:
		reference := str(data, "reference")
		return email{
			to:      str(data, "email"),
			subject: "Consultation fee not paid — " + reference,
			html: layout("Fee not paid", greeting(str(data, "name")),
				fmt.Sprintf(`<p>We could not take your fee of <strong>%s</strong> for the <strong>%s</strong> on %s at %s. No money has left your account.</p>
					<p>Your time is still held. Pay the fee to confirm the appointment.</p>`,
					Money(money(data, "amount")),
					html.EscapeString(str(data, "type_name")),
					html.EscapeString(str(data, "date")),
					html.EscapeString(str(data, "time"))),
				button(siteURL+"/account/consultations", "Pay Fee")),
			text: fmt.Sprintf("We could not take your fee of %s for the %s on %s at %s. No money has left your account, and your time is still held. Pay the fee from your bookings page to confirm the appointment.",
				Money(money(data, "amount")), str(data, "type_name"), str(data, "date"), str(data, "time")),
		}, true

	default:
		return email{}, false
	}
}

// RenderAdmin turns an outbox event into the email the farm receives. It
// returns false for events the farm has no notice about. The address is
// left blank: the worker holds ADMIN_NOTIFICATION_EMAIL and fills it in.
//
// Two kinds live here. A staff-only notice has no customer copy at all —
// a website enquiry, or a late payment asking for a decision. A companion
// notice is the farm's own view of news the customer also receives: a
// booking, or an order.
func RenderAdmin(eventType string, data map[string]any, adminURL string) (email, bool) {
	adminURL = strings.TrimRight(adminURL, "/")

	switch eventType {
	case outbox.TypeConsultationBooked:
		reference := str(data, "reference")
		details := map[string]string{
			"Reference": reference,
			"Date":      str(data, "date"),
			"Time":      str(data, "time"),
			"From":      str(data, "name") + " <" + str(data, "email") + ">",
		}
		if phone := str(data, "phone"); phone != "" {
			details["Phone"] = phone
		}

		// The fee decides what the farm does next: a paid booking is
		// confirmed from the dashboard, an unpaid one is waited on.
		closing := "The customer is waiting for confirmation."
		if fee := money(data, "amount"); fee > 0 {
			details["Fee"] = Money(fee)
			if str(data, "payment_status") == "paid" {
				details["Payment"] = "Fee paid"
				closing = "The fee is paid. Confirm the appointment from the dashboard."
			} else {
				details["Payment"] = "Fee awaiting payment"
				closing = "The slot is held while the fee is outstanding."
			}
		}

		return email{
			subject: "New consultation booked — " + reference,
			html: layout("New consultation booking", "Hello,",
				fmt.Sprintf(`<p>A new <strong>%s</strong> has been booked.</p>
					%s
					<p style="color:`+colourMuted+`;font-size:13px;">%s</p>`,
					html.EscapeString(str(data, "type_name")),
					detailBox(details),
					html.EscapeString(closing)),
				button(adminURL+"/consultations", "Open Bookings")),
			text: fmt.Sprintf("A new %s has been booked under %s for %s at %s.\nCustomer: %s <%s>\n%s",
				str(data, "type_name"), reference, str(data, "date"), str(data, "time"),
				str(data, "name"), str(data, "email"), closing),
		}, true

	case outbox.TypeConsultationCancelled:
		reference := str(data, "reference")
		details := map[string]string{
			"Reference": reference,
			"Date":      str(data, "date"),
			"Time":      str(data, "time"),
			"From":      str(data, "name") + " <" + str(data, "email") + ">",
		}

		return email{
			subject: "Consultation cancelled — " + reference,
			html: layout("Consultation cancelled", "Hello,",
				fmt.Sprintf(`<p>The <strong>%s</strong> booked under <strong>%s</strong> for %s at %s has been cancelled.</p>
					%s`,
					html.EscapeString(str(data, "type_name")),
					html.EscapeString(reference),
					html.EscapeString(str(data, "date")),
					html.EscapeString(str(data, "time")),
					detailBox(details)),
				button(adminURL+"/consultations", "Open Bookings")),
			text: fmt.Sprintf("The %s under %s for %s at %s has been cancelled.\nCustomer: %s <%s>",
				str(data, "type_name"), reference, str(data, "date"), str(data, "time"),
				str(data, "name"), str(data, "email")),
		}, true

	case outbox.TypeConsultationStatus:
		reference := str(data, "reference")
		status := str(data, "status")
		details := map[string]string{
			"Reference": reference,
			"Date":      str(data, "date"),
			"Time":      str(data, "time"),
			"From":      str(data, "name") + " <" + str(data, "email") + ">",
		}

		return email{
			subject: fmt.Sprintf("Booking %s is now %s", reference, status),
			html: layout("Booking update", "Hello,",
				fmt.Sprintf(`<p>The <strong>%s</strong> under <strong>%s</strong> for %s at %s is now <strong>%s</strong>.</p>
					%s`,
					html.EscapeString(str(data, "type_name")),
					html.EscapeString(reference),
					html.EscapeString(str(data, "date")),
					html.EscapeString(str(data, "time")),
					html.EscapeString(status),
					detailBox(details)),
				button(adminURL+"/consultations", "Open Bookings")),
			text: fmt.Sprintf("The %s under %s for %s at %s is now %s.\nCustomer: %s <%s>",
				str(data, "type_name"), reference, str(data, "date"), str(data, "time"), status,
				str(data, "name"), str(data, "email")),
		}, true

	case outbox.TypeOrderCreated:
		number := str(data, "order_number")
		details := map[string]string{
			"Order": number,
			"Total": Money(money(data, "total")),
			"From":  str(data, "name") + " <" + str(data, "email") + ">",
		}

		return email{
			subject: "New order " + number + " from " + str(data, "name"),
			html: layout("New order received", "Hello,",
				fmt.Sprintf(`<p>Order <strong>%s</strong> for <strong>%s</strong> has been placed.</p>
					%s
					<p style="color:`+colourMuted+`;font-size:13px;">The items are held until payment is confirmed.</p>`,
					html.EscapeString(number),
					Money(money(data, "total")),
					detailBox(details)),
				button(adminURL+"/orders", "Open Orders")),
			text: fmt.Sprintf("Order %s from %s <%s> for %s has been placed. The items are held until payment is confirmed.",
				number, str(data, "name"), str(data, "email"), Money(money(data, "total"))),
		}, true

	case outbox.TypeOrderPaid:
		number := str(data, "order_number")
		details := map[string]string{
			"Order": number,
			"Total": Money(money(data, "total")),
			"From":  str(data, "name") + " <" + str(data, "email") + ">",
		}

		return email{
			subject: "Payment confirmed — order " + number,
			html: layout("Payment confirmed", "Hello,",
				fmt.Sprintf(`<p>Payment of <strong>%s</strong> for order <strong>%s</strong> has been confirmed.</p>
					%s
					<p style="color:`+colourMuted+`;font-size:13px;">The order is ready for processing.</p>`,
					Money(money(data, "total")),
					html.EscapeString(number),
					detailBox(details)),
				button(adminURL+"/orders", "Open Orders")),
			text: fmt.Sprintf("Payment of %s for order %s from %s <%s> has been confirmed. The order is ready for processing.",
				Money(money(data, "total")), number, str(data, "name"), str(data, "email")),
		}, true

	case outbox.TypeOrderPaymentUnreconciled:
		number := str(data, "order_number")
		return email{
			subject: "Action needed: late payment on order " + number,
			html: layout("Late payment needs reconciliation", "Hello,",
				fmt.Sprintf(`<p>Order <strong>%s</strong> was paid <strong>%s</strong> after its stock reservation had expired, and the items could not be secured again.</p>
					<p>The order is flagged in the dashboard. Restock the items or refund the payment.</p>`,
					html.EscapeString(number), Money(money(data, "total"))),
				button(adminURL+"/orders", "Open Orders")),
			text: fmt.Sprintf("Order %s was paid %s after its reservation expired and the items could not be secured. Restock the items or refund the payment.",
				number, Money(money(data, "total"))),
		}, true

	case outbox.TypeContactReceived:
		// A visitor has written in and is waiting on a reply, so the
		// sender's address is quoted in the body rather than being the
		// recipient.
		name := str(data, "name")
		emailAddress := str(data, "email")
		phone := str(data, "phone")

		details := map[string]string{
			"From":    name + " <" + emailAddress + ">",
			"Subject": str(data, "subject"),
		}
		if phone != "" {
			details["Phone"] = phone
		}

		footer := ""
		if phone != "" {
			footer = " · " + phone
		}

		return email{
			subject: "Website enquiry: " + str(data, "subject"),
			html: layout("New website enquiry", "Hello,",
				fmt.Sprintf(`<p>A visitor has written in through the contact form.</p>%s
					<p style="white-space:pre-wrap;">%s</p>`,
					detailBox(details), html.EscapeString(str(data, "message"))),
				"mailto:"+html.EscapeString(emailAddress)),
			text: fmt.Sprintf("New website enquiry from %s <%s>%s.\nSubject: %s\n\n%s",
				name, emailAddress, footer, str(data, "subject"), str(data, "message")),
		}, true

	default:
		return email{}, false
	}
}

// greeting opens the message by name when one is known.
func greeting(name string) string {
	if strings.TrimSpace(name) == "" {
		return "<p>Hello,</p>"
	}
	return fmt.Sprintf("<p>Hello %s,</p>", html.EscapeString(name))
}

// detailBox renders a dashed summary panel, as the site does for references.
func detailBox(rows map[string]string) string {
	var b strings.Builder
	b.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" style="width:100%;margin:18px 0;border:1px dashed ` + colourLine + `;border-radius:12px;background:` + colourCream + `;">`)
	b.WriteString(`<tr><td style="padding:16px 18px;">`)

	// A fixed order keeps the panel stable across sends, whichever template
	// filled it: a booking carries a reference and a time, an order its
	// number and amount, an enquiry a sender and a subject.
	for _, key := range []string{"Reference", "Date", "Time", "Fee", "Payment", "Order", "Total", "From", "Phone", "Subject"} {
		value, ok := rows[key]
		if !ok || value == "" {
			continue
		}
		b.WriteString(fmt.Sprintf(
			`<div style="margin:4px 0;font-size:14px;"><span style="color:%s;">%s:</span> <strong>%s</strong></div>`,
			colourMuted, html.EscapeString(key), html.EscapeString(value)))
	}

	b.WriteString(`</td></tr></table>`)
	return b.String()
}

// button renders the call to action.
func button(url, label string) string {
	if url == "" {
		return ""
	}
	return fmt.Sprintf(
		`<table role="presentation" cellpadding="0" cellspacing="0" style="margin:26px 0;"><tr><td style="border-radius:999px;background:%s;">`+
			`<a href="%s" style="display:inline-block;padding:13px 28px;font-weight:700;font-size:14px;color:#ffffff;text-decoration:none;">%s</a>`+
			`</td></tr></table>`,
		colourForest, html.EscapeString(url), html.EscapeString(label))
}

// layout wraps the body in the shared email shell.
func layout(title, greeting, body, cta string) string {
	return fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:0;background:%s;font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;color:#23201A;">
<table role="presentation" cellpadding="0" cellspacing="0" style="width:100%%;padding:28px 12px;">
<tr><td align="center">
<table role="presentation" cellpadding="0" cellspacing="0" style="max-width:560px;width:100%%;background:#ffffff;border:1px solid %s;border-radius:18px;">
<tr><td style="padding:26px 30px;border-bottom:1px solid %s;">
<span style="font-size:17px;font-weight:700;color:%s;">Denisco Global</span>
<span style="display:block;font-size:10.5px;letter-spacing:1.6px;text-transform:uppercase;color:%s;">Agriculture Ltd</span>
</td></tr>
<tr><td style="padding:30px;">
<h1 style="margin:0 0 14px;font-size:21px;color:%s;">%s</h1>
%s
<div style="font-size:14.5px;line-height:1.65;">%s</div>
%s
</td></tr>
<tr><td style="padding:18px 30px;border-top:1px solid %s;font-size:12px;color:%s;">
Denisco Global Agriculture Limited · Integrated agriculture from seed to market
</td></tr>
</table>
</td></tr></table>
</body></html>`,
		colourCream, colourLine, colourLine, colourForest, colourOlive,
		colourForest, html.EscapeString(title), greeting, body, cta,
		colourLine, colourMuted)
}
