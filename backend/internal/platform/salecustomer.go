package platform

// SaleCustomer is the buyer of one Ticket Sale, as the Sales Channel collected
// them, travelling from the point of sale to the Customer upsert. It is the
// whole of what a sale says about the person: who they are, what the sale may be
// declared under, how to reach them, and whether they proved the email address
// is theirs.
//
// It is defined here rather than in either domain module because it crosses a
// module boundary: sales hands it to customers through the UpsertForSale seam,
// and neither module may import the other's packages for a data type — the same
// reason SaleTaxID lives beside its validator.
//
// It is a bundle rather than a parameter list on purpose (#111). Every field is
// a fact about the same person, the upsert authorises its writes by reading
// several of them together, and the shape this replaced — five buyer facts
// passed positionally, grown by appending a sixth — turned each new buyer fact
// into an edit of every signature between the checkout form and the SQL.
type SaleCustomer struct {
	// Email is the Customer's platform-global identity (ADR 0010) and what the
	// upsert keys on. It carries the address as the channel recorded it;
	// normalisation is the customers module's rule and happens at its seam, so
	// no Sales Channel can arrive at a different Customer by casing alone.
	Email     string
	FirstName string
	LastName  string
	// TaxID is the Tax ID this sale was transacted under, unset on a sale that
	// carries none — the `import` channel is the one channel allowed to record a
	// sale without one (ADR 0016).
	TaxID SaleTaxID
	// Phone is the buyer's phone number in canonical E.164 form, empty on every
	// channel that collects none and on an online buyer who skipped the optional
	// field (#106). Empty means exactly "none was collected"; it is never
	// invented and never a placeholder.
	//
	// Unlike the Tax ID it is never snapshotted onto the Ticket Sale — a phone
	// number is no fiscal fact of a sale (#103) — so the Customer profile is the
	// whole of its destination (#107).
	Phone string
	// SelfAsserted reports that this sale was transacted by the person the email
	// belongs to: the checkout ran under that Customer's own full Customer
	// Session, which is proof they control the address. It is false everywhere
	// else — a guest checkout, a Sale Import row, a staff-recorded sale — where
	// what arrives is somebody's account of a purchase rather than the person
	// vouching for themselves.
	//
	// It is the single fact that lets a sale overwrite what a VERIFIED Customer
	// already holds (ADR 0016). Someone editing their own prefilled value is
	// correcting themselves, and their override becomes the new stored
	// assertion; an anonymous visitor typing a known email into a guest checkout
	// is not, and the stored value stands however the sale is recorded.
	//
	// It lives on the buyer rather than on any one of the buyer's attributes
	// because it describes the CHECKOUT, and it is the same proof of email
	// ownership whatever it happens to be protecting. The Tax ID and the phone
	// are both guarded by it (#107), each against the same tampering vector, and
	// until #111 the phone reached it through SaleTaxID — which made a column
	// with nothing to do with Tax IDs depend on one. Anything else a checkout
	// collects on the buyer's own behalf is guarded from here, without going
	// through a neighbouring field.
	//
	// It says nothing about the name, whose rule is unchanged and unaffected:
	// nothing proves a name, so a Verified Customer's name is theirs outright.
	SelfAsserted bool
}
