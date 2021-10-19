// Copyright (c) 2021 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package appstate

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"hash"

	"go.mau.fi/whatsmeow/appstate/lthash"
	waProto "go.mau.fi/whatsmeow/binary/proto"
)

type Mutation struct {
	Operation waProto.SyncdMutation_SyncdMutationSyncdOperation
	Action    *waProto.SyncActionValue
	Index     []string
	IndexMAC  []byte
	ValueMAC  []byte
}

type HashState struct {
	Version   uint64
	Hash      [128]byte
	Mutations []Mutation
}

func NewHashState() HashState {
	return HashState{}
}

func (hs *HashState) updateHash(patch *waProto.SyncdPatch, getPrevSetValueMAC func(indexMAC []byte, maxIndex int) []byte) error {
	var added, removed [][]byte

	for i, mutation := range patch.GetMutations() {
		if mutation.GetOperation() == waProto.SyncdMutation_SET {
			value := mutation.GetRecord().GetValue().GetBlob()
			added = append(added, value[len(value)-32:])
		}
		indexMAC := mutation.GetRecord().GetIndex().GetBlob()
		removal := getPrevSetValueMAC(indexMAC, i)
		if removal != nil {
			removed = append(removed, removal)
		} else if mutation.GetOperation() == waProto.SyncdMutation_REMOVE {
			return ErrMissingPreviousSetValueOperation
		}
	}

	lthash.WAPatchIntegrity.SubtractThenAddInPlace(hs.Hash[:], removed, added)
	return nil
}

func uint64ToBytes(val uint64) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, val)
	return data
}

func concatAndHMAC(alg func() hash.Hash, key []byte, data ...[]byte) []byte {
	h := hmac.New(alg, key)
	for _, item := range data {
		h.Write(item)
	}
	return h.Sum(nil)
}

func (hs *HashState) generateSnapshotMAC(name WAPatchName, key []byte) []byte {
	return concatAndHMAC(sha256.New, key, hs.Hash[:], uint64ToBytes(hs.Version), []byte(name))
}

func generatePatchMAC(patch *waProto.SyncdPatch, name WAPatchName, key []byte) []byte {
	dataToHash := make([][]byte, len(patch.GetMutations())+3)
	dataToHash[0] = patch.GetSnapshotMac()
	for i, mutation := range patch.Mutations {
		val := mutation.GetRecord().GetValue().GetBlob()
		dataToHash[i+1] = val[len(val)-32:]
	}
	dataToHash[len(dataToHash)-2] = uint64ToBytes(patch.GetVersion().GetVersion())
	dataToHash[len(dataToHash)-1] = []byte(name)
	return concatAndHMAC(sha256.New, key, dataToHash...)
}

func generateContentMAC(operation waProto.SyncdMutation_SyncdMutationSyncdOperation, data, keyID, key []byte) []byte {
	operationBytes := []byte{byte(operation) + 1}
	keyDataLength := uint64ToBytes(uint64(len(keyID) + 1))
	return concatAndHMAC(sha512.New, key, operationBytes, keyID, data, keyDataLength)[:32]
}