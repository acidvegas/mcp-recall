// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/handlers/stripe.go

package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"mcprecall/internal/jsonx"
)

var stripeZeroDecimal = map[string]bool{
	"bif": true, "clp": true, "gnf": true, "isk": true, "jpy": true, "kmf": true,
	"krw": true, "mga": true, "pyg": true, "rwf": true, "ugx": true, "vnd": true,
	"xaf": true, "xof": true, "xpf": true,
}

// stripeSymbols approximates Intl.NumberFormat('en-US') currency symbols for the
// common currencies. Unmapped valid currencies fall back to "CODE amount",
// which can differ from Intl's CLDR narrow symbol (documented parity gap).
var stripeSymbols = map[string]string{
	"USD": "$", "CAD": "CA$", "AUD": "A$", "EUR": "€", "GBP": "£",
	"JPY": "¥", "CNY": "CN¥", "INR": "₹", "KRW": "₩", "BRL": "R$",
	"MXN": "MX$", "NZD": "NZ$", "HKD": "HK$", "SGD": "$", "ZAR": "R",
}

func groupThousands(intPart string) string {
	neg := strings.HasPrefix(intPart, "-")
	if neg {
		intPart = intPart[1:]
	}
	n := len(intPart)
	if n <= 3 {
		if neg {
			return "-" + intPart
		}
		return intPart
	}
	var b strings.Builder
	lead := n % 3
	if lead > 0 {
		b.WriteString(intPart[:lead])
	}
	for i := lead; i < n; i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(intPart[i : i+3])
	}
	out := b.String()
	if neg {
		return "-" + out
	}
	return out
}

func stripeFormatAmount(amountVal, currencyVal any) string {
	amount, ok := amountVal.(float64)
	if !ok {
		return ""
	}
	curr := "usd"
	if c, ok := currencyVal.(string); ok {
		curr = strings.ToLower(c)
	}
	zeroDec := stripeZeroDecimal[curr]
	value := amount
	if !zeroDec {
		value = amount / 100
	}
	decimals := 2
	if zeroDec {
		decimals = 0
	}

	neg := value < 0
	abs := value
	if neg {
		abs = -value
	}
	numStr := strconv.FormatFloat(abs, 'f', decimals, 64)
	intPart, fracPart := numStr, ""
	if dot := strings.IndexByte(numStr, '.'); dot >= 0 {
		intPart, fracPart = numStr[:dot], numStr[dot:]
	}
	grouped := groupThousands(intPart) + fracPart

	code := strings.ToUpper(curr)
	var body string
	if sym, ok := stripeSymbols[code]; ok {
		body = sym + grouped
	} else {
		body = code + " " + grouped
	}
	if neg {
		return "-" + body
	}
	return body
}

func objIsTrue(o *jsonx.Obj, key string) bool {
	v, ok := o.Get(key)
	b, _ := v.(bool)
	return ok && b
}

func objIsFalse(o *jsonx.Obj, key string) bool {
	v, ok := o.Get(key)
	if !ok {
		return false
	}
	b, isBool := v.(bool)
	return isBool && !b
}

func stripeCustomer(item *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(item, "id"); ok {
		p = append(p, s)
	}
	if s, ok := objStr(item, "name"); ok && s != "" {
		p = append(p, `"`+s+`"`)
	}
	if s, ok := objStr(item, "email"); ok {
		p = append(p, s)
	}
	if s, ok := objStr(item, "phone"); ok && s != "" {
		p = append(p, s)
	}
	if objIsTrue(item, "delinquent") {
		p = append(p, "[delinquent]")
	}
	return strings.Join(p, " · ")
}

func stripeInvoice(item *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(item, "id"); ok {
		p = append(p, s)
	}
	if s, ok := objStr(item, "status"); ok {
		p = append(p, "["+s+"]")
	}
	curr, _ := item.Get("currency")
	if due, ok := objNum(item, "amount_due"); ok {
		p = append(p, "due: "+stripeFormatAmount(due, curr))
	}
	if paid, ok := objNum(item, "amount_paid"); ok && paid > 0 {
		p = append(p, "paid: "+stripeFormatAmount(paid, curr))
	}
	nameVal, ok := item.Get("customer_name")
	if !ok {
		nameVal, _ = item.Get("customer_email")
	}
	if s, ok := nameVal.(string); ok && s != "" {
		p = append(p, s)
	}
	if s, ok := objStr(item, "billing_reason"); ok {
		p = append(p, "reason: "+s)
	}
	return strings.Join(p, " · ")
}

func stripePaymentIntent(item *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(item, "id"); ok {
		p = append(p, s)
	}
	amtVal, _ := item.Get("amount")
	currVal, _ := item.Get("currency")
	p = append(p, stripeFormatAmount(amtVal, currVal))
	if s, ok := objStr(item, "status"); ok {
		p = append(p, "["+s+"]")
	}
	if s, ok := objStr(item, "customer"); ok && s != "" {
		p = append(p, "customer: "+s)
	}
	if s, ok := objStr(item, "description"); ok && s != "" {
		p = append(p, firstChars(s, 100))
	}
	return strings.Join(filterEmpty(p), " · ")
}

func stripeSubscription(item *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(item, "id"); ok {
		p = append(p, s)
	}
	if s, ok := objStr(item, "status"); ok {
		p = append(p, "["+s+"]")
	}
	if planVal, ok := item.Get("plan"); ok {
		if plan, ok := planVal.(*jsonx.Obj); ok {
			if s, ok := objStr(plan, "id"); ok {
				p = append(p, "plan: "+s)
			}
			curr, ok := plan.Get("currency")
			if !ok {
				curr, _ = item.Get("currency")
			}
			amtVal, _ := plan.Get("amount")
			if a := stripeFormatAmount(amtVal, curr); a != "" {
				p = append(p, a)
			}
		}
	}
	if s, ok := objStr(item, "customer"); ok {
		p = append(p, "customer: "+s)
	}
	if objIsTrue(item, "cancel_at_period_end") {
		p = append(p, "cancels at period end")
	}
	return strings.Join(p, " · ")
}

func stripeProduct(item *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(item, "id"); ok {
		p = append(p, s)
	}
	if s, ok := objStr(item, "name"); ok {
		p = append(p, `"`+s+`"`)
	}
	if objIsFalse(item, "active") {
		p = append(p, "[inactive]")
	}
	if s, ok := objStr(item, "description"); ok && s != "" {
		p = append(p, firstChars(s, 100))
	}
	return strings.Join(p, " · ")
}

func stripePrice(item *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(item, "id"); ok {
		p = append(p, s)
	}
	amtVal, _ := item.Get("unit_amount")
	currVal, _ := item.Get("currency")
	if a := stripeFormatAmount(amtVal, currVal); a != "" {
		p = append(p, a)
	}
	if recVal, ok := item.Get("recurring"); ok {
		if rec, ok := recVal.(*jsonx.Obj); ok {
			interval, _ := objStr(rec, "interval")
			// TS: count && count !== 1 ? `every ${count} ${interval}s` : `per ${interval}`
			if c, ok := objNum(rec, "interval_count"); ok && c != 0 && c != 1 {
				p = append(p, fmt.Sprintf("every %s %ss", jsonx.Number(c), interval))
			} else {
				p = append(p, "per "+interval)
			}
		}
	}
	if s, ok := objStr(item, "product"); ok {
		p = append(p, "product: "+s)
	}
	return strings.Join(p, " · ")
}

func stripeDispute(item *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(item, "id"); ok {
		p = append(p, s)
	}
	amtVal, _ := item.Get("amount")
	currVal, _ := item.Get("currency")
	if a := stripeFormatAmount(amtVal, currVal); a != "" {
		p = append(p, a)
	}
	if s, ok := objStr(item, "status"); ok {
		p = append(p, "["+s+"]")
	}
	if s, ok := objStr(item, "reason"); ok {
		p = append(p, "reason: "+s)
	}
	if s, ok := objStr(item, "charge"); ok {
		p = append(p, "charge: "+s)
	}
	return strings.Join(p, " · ")
}

func stripePaymentLink(o *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(o, "id"); ok {
		p = append(p, s)
	}
	if s, ok := objStr(o, "url"); ok {
		p = append(p, s)
	}
	if objIsFalse(o, "active") {
		p = append(p, "[inactive]")
	}
	return strings.Join(p, " · ")
}

func stripeBalance(o *jsonx.Obj) string {
	var lines []string
	if avail, ok := objArr(o, "available"); ok {
		for _, b := range avail {
			if bo, ok := b.(*jsonx.Obj); ok {
				amt, _ := bo.Get("amount")
				curr, _ := bo.Get("currency")
				lines = append(lines, "available: "+stripeFormatAmount(amt, curr))
			}
		}
	}
	if pend, ok := objArr(o, "pending"); ok {
		for _, b := range pend {
			if bo, ok := b.(*jsonx.Obj); ok {
				if amt, ok := objNum(bo, "amount"); ok && amt != 0 {
					curr, _ := bo.Get("currency")
					lines = append(lines, "pending: "+stripeFormatAmount(amt, curr))
				}
			}
		}
	}
	if len(lines) == 0 {
		return "Balance: $0.00"
	}
	return strings.Join(lines, " · ")
}

func stripeAccount(o *jsonx.Obj) string {
	var p []string
	if s, ok := objStr(o, "id"); ok {
		p = append(p, s)
	}
	if s, ok := objStr(o, "display_name"); ok {
		p = append(p, `"`+s+`"`)
	}
	if s, ok := objStr(o, "email"); ok {
		p = append(p, s)
	}
	if s, ok := objStr(o, "country"); ok {
		p = append(p, s)
	}
	return strings.Join(p, " · ")
}

func stripeByObjectType(item *jsonx.Obj) string {
	obj, _ := objStr(item, "object")
	switch obj {
	case "customer":
		return stripeCustomer(item)
	case "invoice":
		return stripeInvoice(item)
	case "payment_intent":
		return stripePaymentIntent(item)
	case "subscription":
		return stripeSubscription(item)
	case "product":
		return stripeProduct(item)
	case "price":
		return stripePrice(item)
	case "dispute":
		return stripeDispute(item)
	case "payment_link":
		return stripePaymentLink(item)
	default:
		var p []string
		if s, ok := objStr(item, "id"); ok {
			p = append(p, s)
		}
		if s, ok := objStr(item, "object"); ok {
			p = append(p, "["+s+"]")
		}
		if s, ok := objStr(item, "status"); ok {
			p = append(p, "["+s+"]")
		}
		return strings.Join(p, " · ")
	}
}

func stripePickSummariser(suffix string) func(*jsonx.Obj) string {
	switch {
	case strings.Contains(suffix, "customer"):
		return stripeCustomer
	case strings.Contains(suffix, "invoice"):
		return stripeInvoice
	case strings.Contains(suffix, "payment_intent"):
		return stripePaymentIntent
	case strings.Contains(suffix, "subscription"):
		return stripeSubscription
	case strings.Contains(suffix, "product"):
		return stripeProduct
	case strings.Contains(suffix, "price"):
		return stripePrice
	case strings.Contains(suffix, "dispute"):
		return stripeDispute
	case strings.Contains(suffix, "payment_link"):
		return stripePaymentLink
	}
	return stripeByObjectType
}

const stripeMaxItems = 10

func stripeSummariseList(items []any, summarise func(*jsonx.Obj) string) string {
	n := len(items)
	if n > stripeMaxItems {
		n = stripeMaxItems
	}
	var lines []string
	for _, item := range items[:n] {
		if o, ok := item.(*jsonx.Obj); ok {
			lines = append(lines, summarise(o))
		} else {
			lines = append(lines, jsToString(item))
		}
	}
	overflow := ""
	if len(items) > stripeMaxItems {
		overflow = fmt.Sprintf("\n…and %d more", len(items)-stripeMaxItems)
	}
	return strings.Join(lines, "\n") + overflow
}

func filterEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func stripeHandler(toolName string, output any) Result {
	raw := ExtractText(output)
	originalSize := byteLen(raw)

	parts := strings.Split(toolName, "__")
	suffix := parts[len(parts)-1]

	switch suffix {
	case "retrieve_balance":
		parsed, err := jsonx.ParseString(raw)
		if err != nil {
			return Result{Summary: firstChars(raw, 500), OriginalSize: originalSize}
		}
		o, _ := parsed.(*jsonx.Obj)
		return Result{Summary: stripeBalance(o), OriginalSize: originalSize}
	case "get_stripe_account_info":
		parsed, err := jsonx.ParseString(raw)
		if err != nil {
			return Result{Summary: firstChars(raw, 500), OriginalSize: originalSize}
		}
		o, _ := parsed.(*jsonx.Obj)
		return Result{Summary: stripeAccount(o), OriginalSize: originalSize}
	case "search_stripe_documentation":
		return Result{Summary: trimEnd(firstChars(raw, 500)), OriginalSize: originalSize}
	}

	parsed, err := jsonx.ParseString(raw)
	if err != nil {
		return Result{Summary: trimEnd(firstChars(raw, 500)), OriginalSize: originalSize}
	}

	summarise := stripePickSummariser(suffix)

	if obj, ok := parsed.(*jsonx.Obj); ok {
		if data, ok := objArr(obj, "data"); ok {
			if len(data) == 0 {
				return Result{Summary: "No items.", OriginalSize: originalSize}
			}
			return Result{Summary: stripeSummariseList(data, summarise), OriginalSize: originalSize}
		}
	}
	if arr, ok := parsed.([]any); ok {
		if len(arr) == 0 {
			return Result{Summary: "No items.", OriginalSize: originalSize}
		}
		return Result{Summary: stripeSummariseList(arr, summarise), OriginalSize: originalSize}
	}
	if o, ok := parsed.(*jsonx.Obj); ok {
		return Result{Summary: summarise(o), OriginalSize: originalSize}
	}
	return Result{Summary: jsToString(parsed), OriginalSize: originalSize}
}
