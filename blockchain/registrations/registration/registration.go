package registration

import (
	"pandora-pay/helpers"
)

type Registration struct {
	helpers.SerializableInterface `json:"-"`
	PublicKey                     []byte `json:"-"` //hashmap key
	Index                         uint64
}

func (registration *Registration) Serialize(w *helpers.BufferWriter) {
	w.WriteUvarint(registration.Index)
}

func (registration *Registration) SerializeToBytes() []byte {
	w := helpers.NewBufferWriter()
	registration.Serialize(w)
	return w.Bytes()
}

func (registration *Registration) Deserialize(r *helpers.BufferReader) (err error) {
	if registration.Index, err = r.ReadUvarint(); err != nil {
		return
	}
	return
}

func NewRegistration(publicKey []byte) *Registration {

	return &Registration{
		PublicKey: publicKey,
	}

	return nil
}
