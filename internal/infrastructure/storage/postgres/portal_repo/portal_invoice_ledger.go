package portal_repo

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"metapus/internal/application/portalapp"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type invoiceLedgerPayment struct {
	amount, fee, net types.CryptoAmount
	posted           bool
	version          int
	generations      map[int]invoiceLedgerGeneration
}

type invoiceLedgerGeneration struct {
	gross, net, fee                                   string
	currentGross, currentNet, currentFee, reversedNet string
}

func knownInvoiceLedger(net, fee, reversed string) portalapp.PortalInvoiceLedger {
	return portalapp.PortalInvoiceLedger{Status: "known", CreditedNetAmount: &net, ProcessingFeeAmount: &fee, ReversedNetAmount: &reversed}
}
func inconsistentInvoiceLedger() portalapp.PortalInvoiceLedger {
	return portalapp.PortalInvoiceLedger{Status: "inconsistent"}
}

// The roots are immutable original postings. Reversals are attributed by line,
// not their recorder ID (adjustments and reorgs have independent recorders).
// Each root and linked reversal appears once, even across posting generations.
func invoiceLedgerQuery() string {
	// The payment's custody side is the attribution of its receipt: a suspense
	// expense, signed here as the received gross.
	type register struct{ kind, table, dimensions, extra, received string }
	registers := []register{
		{"custody", "reg_crypto_receipt_suspense_movements", "m.receipt_id=p.receipt_id AND m.token_id=p.token_id", "", "expense"},
		{"merchant", "reg_crypto_merchant_balance_movements", "m.merchant_id=p.merchant_id AND m.token_id=p.token_id", "", "receipt"},
		{"fee", "reg_crypto_fee_movements", "m.merchant_id=p.merchant_id AND m.token_id=p.token_id", "AND m.fee_type='processing'", "receipt"},
	}
	roots, reversals := make([]string, 0, 3), make([]string, 0, 3)
	for _, reg := range registers {
		roots = append(roots, fmt.Sprintf(`SELECT '%s' AS kind,p.id AS payment_id,m.recorder_version AS version,m.line_id,
   CASE WHEN m.record_type='%s' THEN m.amount ELSE -m.amount END AS amount
   FROM %s m JOIN payments p ON m.recorder_id=p.id
   WHERE m.recorder_type='CryptoPayment' AND m.reversal_of_line_id IS NULL AND %s %s`, reg.kind, reg.received, reg.table, reg.dimensions, reg.extra))
		reversals = append(reversals, fmt.Sprintf(`SELECT r.kind,r.payment_id,r.version,
   CASE WHEN m.record_type='%s' THEN m.amount ELSE -m.amount END AS amount,FALSE AS original
   FROM roots r JOIN %s m ON m.reversal_of_line_id=r.line_id
   JOIN payments p ON p.id=r.payment_id WHERE r.kind='%s' AND %s %s`, reg.received, reg.table, reg.kind, reg.dimensions, reg.extra))
	}
	return `WITH payments AS (SELECT id,merchant_id,token_id,wallet_id,receipt_id FROM doc_crypto_payments
  WHERE invoice_id=$1 AND merchant_id=$2 AND token_id=$3), roots AS (` + strings.Join(roots, " UNION ALL ") + `),
  entries AS (SELECT kind,payment_id,version,amount,TRUE AS original FROM roots UNION ALL ` + strings.Join(reversals, " UNION ALL ") + `)
  SELECT payment_id,version,
  COALESCE(SUM(amount) FILTER(WHERE kind='custody' AND original),0)::text,
  COALESCE(SUM(amount) FILTER(WHERE kind='merchant' AND original),0)::text,
  COALESCE(SUM(amount) FILTER(WHERE kind='fee' AND original),0)::text,
  COALESCE(SUM(amount) FILTER(WHERE kind='custody'),0)::text,
  COALESCE(SUM(amount) FILTER(WHERE kind='merchant'),0)::text,
  COALESCE(SUM(amount) FILTER(WHERE kind='fee'),0)::text,
  COALESCE(SUM(-amount) FILTER(WHERE kind='merchant' AND NOT original AND amount<0),0)::text
  FROM entries GROUP BY payment_id,version`
}

func (r *DashboardRepo) loadInvoiceLedger(ctx context.Context, invoiceID, merchantID, tokenID id.ID) (map[string]portalapp.PortalInvoiceLedger, portalapp.PortalInvoiceLedger, error) {
	q := querier(ctx)
	rows, err := q.Query(ctx, `SELECT id,amount,fee_fixed,fee_percent_bp,fee_min,fee_max,posted,posted_version
  FROM doc_crypto_payments WHERE invoice_id=$1 AND merchant_id=$2 AND token_id=$3`, invoiceID, merchantID, tokenID)
	if err != nil {
		return nil, portalapp.PortalInvoiceLedger{}, fmt.Errorf("invoice ledger payments: %w", err)
	}
	payments := make(map[string]*invoiceLedgerPayment)
	for rows.Next() {
		var pid id.ID
		var p invoiceLedgerPayment
		var fixed, minFee, maxFee types.CryptoAmount
		var bp int
		if err := rows.Scan(&pid, &p.amount, &fixed, &bp, &minFee, &maxFee, &p.posted, &p.version); err != nil {
			rows.Close()
			return nil, portalapp.PortalInvoiceLedger{}, err
		}
		p.fee = calculateFee(fixed, bp, minFee, maxFee, p.amount)
		p.net = types.ZeroCryptoAmount()
		if p.amount.Cmp(p.fee) > 0 {
			p.net = p.amount.Sub(p.fee)
		}
		p.generations = make(map[int]invoiceLedgerGeneration)
		payments[pid.String()] = &p
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, portalapp.PortalInvoiceLedger{}, err
	}
	rows, err = q.Query(ctx, invoiceLedgerQuery(), invoiceID, merchantID, tokenID)
	if err != nil {
		return nil, portalapp.PortalInvoiceLedger{}, fmt.Errorf("invoice ledger movements: %w", err)
	}
	for rows.Next() {
		var pid id.ID
		var version int
		var g invoiceLedgerGeneration
		if err := rows.Scan(&pid, &version, &g.gross, &g.net, &g.fee, &g.currentGross, &g.currentNet, &g.currentFee, &g.reversedNet); err != nil {
			rows.Close()
			return nil, portalapp.PortalInvoiceLedger{}, err
		}
		if p := payments[pid.String()]; p != nil {
			p.generations[version] = g
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, portalapp.PortalInvoiceLedger{}, err
	}
	return summarizeInvoiceLedger(payments)
}

func summarizeInvoiceLedger(payments map[string]*invoiceLedgerPayment) (map[string]portalapp.PortalInvoiceLedger, portalapp.PortalInvoiceLedger, error) {
	ledgers := make(map[string]portalapp.PortalInvoiceLedger, len(payments))
	totalNet, totalFee, totalReversed := new(big.Int), new(big.Int), new(big.Int)
	consistent := true
	for pid, p := range payments {
		net, fee, reversed := new(big.Int), new(big.Int), new(big.Int)
		valid := true
		if p.posted || p.version > 0 {
			_, valid = p.generations[p.version]
		}
		for _, g := range p.generations {
			if g.gross != p.amount.String() || g.net != p.net.String() || g.fee != p.fee.String() {
				valid = false
			}
			for _, raw := range []string{g.currentGross, g.currentNet, g.currentFee, g.reversedNet} {
				n, ok := new(big.Int).SetString(raw, 10)
				if !ok || n.Sign() < 0 {
					valid = false
				}
			}
			if !valid {
				continue
			}
			n, _ := new(big.Int).SetString(g.currentNet, 10)
			net.Add(net, n)
			n, _ = new(big.Int).SetString(g.currentFee, 10)
			fee.Add(fee, n)
			n, _ = new(big.Int).SetString(g.reversedNet, 10)
			reversed.Add(reversed, n)
		}
		if !valid {
			ledgers[pid] = inconsistentInvoiceLedger()
			consistent = false
			continue
		}
		ledgers[pid] = knownInvoiceLedger(net.String(), fee.String(), reversed.String())
		totalNet.Add(totalNet, net)
		totalFee.Add(totalFee, fee)
		totalReversed.Add(totalReversed, reversed)
	}
	if !consistent {
		return ledgers, inconsistentInvoiceLedger(), nil
	}
	return ledgers, knownInvoiceLedger(totalNet.String(), totalFee.String(), totalReversed.String()), nil
}
