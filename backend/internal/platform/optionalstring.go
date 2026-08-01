package platform

import "encoding/json"

// OptionalString distinguishes the three things a JSON field can say: nothing at
// all, an explicit null or blank, and a value. encoding/json flattens the first
// two into a nil *string, which is enough for almost every field in this system
// and not enough for one that can be *cleared*: on such a field an absent key
// and a null key must mean opposite things — "leave what you hold" and "let it
// go" — and a pointer cannot hold both.
//
// Present is set by UnmarshalJSON, which the decoder calls only for a key that
// actually appeared in the document, so the zero value is "absent" and needs no
// help from the caller.
//
// It is a decoding device and never a wire shape. A field declared with this
// type must carry `swaggertype:"string"`, or the generated contract would
// publish these two Go fields as an object nobody sends.
//
// It lives here rather than in either handler that uses it because it holds no
// domain: it is a JSON decoding primitive, and this package is where the others
// live. Its callers are the Customer profile's phone number and the
// Organization's Support WhatsApp — both optional phone numbers their owner is
// entitled to withdraw, which is exactly the shape that needs the third state.
type OptionalString struct {
	Present bool
	Value   *string
}

func (o *OptionalString) UnmarshalJSON(data []byte) error {
	o.Present = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}
