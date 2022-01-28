package hooks_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/ElrondNetwork/elrond-go-core/core"
	"github.com/ElrondNetwork/elrond-go-core/data"
	"github.com/ElrondNetwork/elrond-go-core/data/block"
	"github.com/ElrondNetwork/elrond-go-core/data/esdt"
	"github.com/ElrondNetwork/elrond-go/dataRetriever"
	"github.com/ElrondNetwork/elrond-go/process"
	"github.com/ElrondNetwork/elrond-go/process/mock"
	"github.com/ElrondNetwork/elrond-go/process/smartContract/hooks"
	"github.com/ElrondNetwork/elrond-go/state"
	"github.com/ElrondNetwork/elrond-go/storage"
	"github.com/ElrondNetwork/elrond-go/testscommon"
	dataRetrieverMock "github.com/ElrondNetwork/elrond-go/testscommon/dataRetriever"
	stateMock "github.com/ElrondNetwork/elrond-go/testscommon/state"
	"github.com/ElrondNetwork/elrond-go/testscommon/trie"
	vmcommon "github.com/ElrondNetwork/elrond-vm-common"
	vmcommonBuiltInFunctions "github.com/ElrondNetwork/elrond-vm-common/builtInFunctions"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createMockBlockChainHookArgs() hooks.ArgBlockChainHook {
	datapool := dataRetrieverMock.NewPoolsHolderMock()
	arguments := hooks.ArgBlockChainHook{
		Accounts: &stateMock.AccountsStub{
			GetExistingAccountCalled: func(address []byte) (handler vmcommon.AccountHandler, e error) {
				return &mock.AccountWrapMock{}, nil
			},
		},
		PubkeyConv:         mock.NewPubkeyConverterMock(32),
		StorageService:     &mock.ChainStorerMock{},
		BlockChain:         &mock.BlockChainStub{},
		ShardCoordinator:   mock.NewOneShardCoordinatorMock(),
		Marshalizer:        &mock.MarshalizerMock{},
		Uint64Converter:    &mock.Uint64ByteSliceConverterMock{},
		BuiltInFunctions:   vmcommonBuiltInFunctions.NewBuiltInFunctionContainer(),
		NFTStorageHandler:  &testscommon.SimpleNFTStorageHandlerStub{},
		DataPool:           datapool,
		CompiledSCPool:     datapool.SmartContracts(),
		EpochNotifier:      &mock.EpochNotifierStub{},
		NilCompiledSCStore: true,
	}
	return arguments
}

func TestNewBlockChainHookImpl_NilAccountsAdapterShouldErr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args        func() hooks.ArgBlockChainHook
		expectedErr error
	}{
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.Accounts = nil
				return args
			},
			expectedErr: process.ErrNilAccountsAdapter,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.PubkeyConv = nil
				return args
			},
			expectedErr: process.ErrNilPubkeyConverter,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.StorageService = nil
				return args
			},
			expectedErr: process.ErrNilStorage,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.BlockChain = nil
				return args
			},
			expectedErr: process.ErrNilBlockChain,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.ShardCoordinator = nil
				return args
			},
			expectedErr: process.ErrNilShardCoordinator,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.Marshalizer = nil
				return args
			},
			expectedErr: process.ErrNilMarshalizer,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.Uint64Converter = nil
				return args
			},
			expectedErr: process.ErrNilUint64Converter,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.BuiltInFunctions = nil
				return args
			},
			expectedErr: process.ErrNilBuiltInFunction,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.CompiledSCPool = nil
				return args
			},
			expectedErr: process.ErrNilCacher,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.NFTStorageHandler = nil
				return args
			},
			expectedErr: process.ErrNilNFTStorageHandler,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				args := createMockBlockChainHookArgs()
				args.EpochNotifier = nil
				return args
			},
			expectedErr: process.ErrNilEpochNotifier,
		},
		{
			args: func() hooks.ArgBlockChainHook {
				return createMockBlockChainHookArgs()
			},
			expectedErr: nil,
		},
	}

	for _, test := range tests {
		bh, err := hooks.NewBlockChainHookImpl(test.args())
		require.Equal(t, test.expectedErr, err)

		if test.expectedErr != nil {
			require.Nil(t, bh)
		} else {
			require.NotNil(t, bh)
		}
	}
}

func TestBlockChainHookImpl_GetCode(t *testing.T) {
	t.Parallel()

	args := createMockBlockChainHookArgs()

	t.Run("nil account expect nil code", func(t *testing.T) {
		bh, _ := hooks.NewBlockChainHookImpl(args)

		code := bh.GetCode(nil)
		require.Nil(t, code)
	})

	t.Run("expect correct returned code", func(t *testing.T) {
		expectedCodeHash := []byte("codeHash")
		expectedCode := []byte("code")

		args.Accounts = &stateMock.AccountsStub{
			GetCodeCalled: func(codeHash []byte) []byte {
				require.Equal(t, expectedCodeHash, codeHash)
				return expectedCode
			},
		}
		bh, _ := hooks.NewBlockChainHookImpl(args)

		account, _ := state.NewUserAccount([]byte("address"))
		account.SetCodeHash(expectedCodeHash)

		code := bh.GetCode(account)
		require.Equal(t, expectedCode, code)
	})
}

func TestBlTestBlockChainHookImpl_GetUserAccountNotASystemAccountInCrossShard(t *testing.T) {
	t.Parallel()

	args := createMockBlockChainHookArgs()
	args.ShardCoordinator = &mock.ShardCoordinatorStub{
		ComputeIdCalled: func(address []byte) uint32 {
			return 0
		},
		SelfIdCalled: func() uint32 {
			return 1
		},
	}
	args.Accounts = &stateMock.AccountsStub{}
	bh, _ := hooks.NewBlockChainHookImpl(args)
	addr := bytes.Repeat([]byte{0}, 32)
	_, err := bh.GetUserAccount(addr)
	assert.Equal(t, state.ErrAccNotFound, err)
}

func TestBlTestBlockChainHookImpl_GetUserAccountGetAccFromAddressErr(t *testing.T) {
	t.Parallel()

	errExpected := errors.New("expected err")

	args := createMockBlockChainHookArgs()
	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(address []byte) (handler vmcommon.AccountHandler, e error) {
			return nil, errExpected
		},
	}
	bh, _ := hooks.NewBlockChainHookImpl(args)
	_, err := bh.GetUserAccount(make([]byte, 0))
	assert.Equal(t, errExpected, err)
}

func TestBlTestBlockChainHookImpl_GetUserAccountWrongTypeShouldErr(t *testing.T) {
	t.Parallel()

	args := createMockBlockChainHookArgs()
	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(address []byte) (handler vmcommon.AccountHandler, e error) {
			return &mock.PeerAccountHandlerMock{}, nil
		},
	}
	bh, _ := hooks.NewBlockChainHookImpl(args)
	_, err := bh.GetUserAccount(make([]byte, 0))
	assert.Equal(t, state.ErrWrongTypeAssertion, err)
}

func TestBlTestBlockChainHookImpl_GetUserAccount(t *testing.T) {
	t.Parallel()

	expectedAccount, _ := state.NewUserAccount([]byte("1234"))
	args := createMockBlockChainHookArgs()
	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(address []byte) (handler vmcommon.AccountHandler, e error) {
			return expectedAccount, nil
		},
	}
	bh, _ := hooks.NewBlockChainHookImpl(args)
	acc, err := bh.GetUserAccount(expectedAccount.Address)

	assert.Nil(t, err)
	assert.Equal(t, expectedAccount, acc)
}

func TestBlockChainHookImpl_GetStorageAccountErrorsShouldErr(t *testing.T) {
	t.Parallel()

	errExpected := errors.New("expected err")

	args := createMockBlockChainHookArgs()
	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(address []byte) (handler vmcommon.AccountHandler, e error) {
			return nil, errExpected
		},
	}
	bh, _ := hooks.NewBlockChainHookImpl(args)

	value, err := bh.GetStorageData(make([]byte, 0), make([]byte, 0))

	assert.Equal(t, errExpected, err)
	assert.Nil(t, value)
}

func TestBlockChainHookImpl_GetStorageDataShouldWork(t *testing.T) {
	t.Parallel()

	variableIdentifier := []byte("variable")
	variableValue := []byte("value")
	accnt := mock.NewAccountWrapMock(nil)
	_ = accnt.DataTrieTracker().SaveKeyValue(variableIdentifier, variableValue)

	args := createMockBlockChainHookArgs()
	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(address []byte) (handler vmcommon.AccountHandler, e error) {
			return accnt, nil
		},
	}
	bh, _ := hooks.NewBlockChainHookImpl(args)

	value, err := bh.GetStorageData(make([]byte, 0), variableIdentifier)

	assert.Nil(t, err)
	assert.Equal(t, variableValue, value)
}

func TestBlockChainHookImpl_NewAddressLengthNoGood(t *testing.T) {
	t.Parallel()

	acnts := &stateMock.AccountsStub{}
	acnts.GetExistingAccountCalled = func(address []byte) (vmcommon.AccountHandler, error) {
		return state.NewUserAccount(address)
	}
	args := createMockBlockChainHookArgs()
	args.Accounts = acnts
	bh, _ := hooks.NewBlockChainHookImpl(args)

	address := []byte("test")
	nonce := uint64(10)

	scAddress, err := bh.NewAddress(address, nonce, []byte("00"))
	assert.Equal(t, hooks.ErrAddressLengthNotCorrect, err)
	assert.Nil(t, scAddress)

	address = []byte("1234567890123456789012345678901234567890")
	scAddress, err = bh.NewAddress(address, nonce, []byte("00"))
	assert.Equal(t, hooks.ErrAddressLengthNotCorrect, err)
	assert.Nil(t, scAddress)
}

func TestBlockChainHookImpl_NewAddressVMTypeTooLong(t *testing.T) {
	t.Parallel()

	acnts := &stateMock.AccountsStub{}
	acnts.GetExistingAccountCalled = func(address []byte) (vmcommon.AccountHandler, error) {
		return state.NewUserAccount(address)
	}
	args := createMockBlockChainHookArgs()
	args.Accounts = acnts
	bh, _ := hooks.NewBlockChainHookImpl(args)

	address := []byte("01234567890123456789012345678900")
	nonce := uint64(10)

	vmType := []byte("010")
	scAddress, err := bh.NewAddress(address, nonce, vmType)
	assert.Equal(t, hooks.ErrVMTypeLengthIsNotCorrect, err)
	assert.Nil(t, scAddress)
}

func TestBlockChainHookImpl_NewAddress(t *testing.T) {
	t.Parallel()

	acnts := &stateMock.AccountsStub{}
	acnts.GetExistingAccountCalled = func(address []byte) (vmcommon.AccountHandler, error) {
		return state.NewUserAccount(address)
	}
	args := createMockBlockChainHookArgs()
	args.Accounts = acnts
	bh, _ := hooks.NewBlockChainHookImpl(args)

	address := []byte("01234567890123456789012345678900")
	nonce := uint64(10)

	vmType := []byte("11")
	scAddress1, err := bh.NewAddress(address, nonce, vmType)
	assert.Nil(t, err)

	for i := 0; i < 8; i++ {
		assert.Equal(t, scAddress1[i], uint8(0))
	}
	assert.True(t, bytes.Equal(vmType, scAddress1[8:10]))

	nonce++
	scAddress2, err := bh.NewAddress(address, nonce, []byte("00"))
	assert.Nil(t, err)

	assert.False(t, bytes.Equal(scAddress1, scAddress2))

	fmt.Printf("%s \n%s \n", hex.EncodeToString(scAddress1), hex.EncodeToString(scAddress2))
}

func TestBlockChainHookImpl_GetBlockhashShouldReturnCurrentBlockHeaderHash(t *testing.T) {
	t.Parallel()

	hdrToRet := &block.Header{Nonce: 2}
	hashToRet := []byte("hash")
	args := createMockBlockChainHookArgs()
	args.BlockChain = &mock.BlockChainStub{
		GetCurrentBlockHeaderCalled: func() data.HeaderHandler {
			return hdrToRet
		},
		GetCurrentBlockHeaderHashCalled: func() []byte {
			return hashToRet
		},
	}
	bh, _ := hooks.NewBlockChainHookImpl(args)

	hash, err := bh.GetBlockhash(2)
	assert.Nil(t, err)
	assert.Equal(t, hashToRet, hash)
}

func TestBlockChainHookImpl_GetBlockhashFromOldEpoch(t *testing.T) {
	t.Parallel()

	hdrToRet := &block.Header{Nonce: 2, Epoch: 2}
	hashToRet := []byte("hash")
	args := createMockBlockChainHookArgs()

	marshaledData, _ := args.Marshalizer.Marshal(hdrToRet)

	args.BlockChain = &mock.BlockChainStub{
		GetCurrentBlockHeaderCalled: func() data.HeaderHandler {
			return &block.Header{Nonce: 10, Epoch: 10}
		},
	}
	args.StorageService = &mock.ChainStorerMock{
		GetStorerCalled: func(unitType dataRetriever.UnitType) storage.Storer {
			if uint8(unitType) >= uint8(dataRetriever.ShardHdrNonceHashDataUnit) {
				return &testscommon.StorerStub{
					GetCalled: func(key []byte) ([]byte, error) {
						return hashToRet, nil
					},
				}
			}

			return &testscommon.StorerStub{
				GetCalled: func(key []byte) ([]byte, error) {
					return marshaledData, nil
				},
			}
		},
	}
	bh, _ := hooks.NewBlockChainHookImpl(args)

	_, err := bh.GetBlockhash(2)
	assert.Equal(t, err, process.ErrInvalidBlockRequestOldEpoch)
}

func TestBlockChainHookImpl_GettersFromBlockchainCurrentHeader(t *testing.T) {
	t.Parallel()

	nonce := uint64(37)
	round := uint64(5)
	timestamp := uint64(1234)
	randSeed := []byte("a")
	rootHash := []byte("b")
	epoch := uint32(7)
	hdrToRet := &block.Header{
		Nonce:     nonce,
		Round:     round,
		TimeStamp: timestamp,
		RandSeed:  randSeed,
		RootHash:  rootHash,
		Epoch:     epoch,
	}

	args := createMockBlockChainHookArgs()
	args.BlockChain = &mock.BlockChainStub{
		GetCurrentBlockHeaderCalled: func() data.HeaderHandler {
			return hdrToRet
		},
	}

	bh, _ := hooks.NewBlockChainHookImpl(args)

	assert.Equal(t, nonce, bh.LastNonce())
	assert.Equal(t, round, bh.LastRound())
	assert.Equal(t, timestamp, bh.LastTimeStamp())
	assert.Equal(t, epoch, bh.LastEpoch())
	assert.Equal(t, randSeed, bh.LastRandomSeed())
	assert.Equal(t, rootHash, bh.GetStateRootHash())
}

func TestBlockChainHookImpl_GettersFromCurrentHeader(t *testing.T) {
	t.Parallel()

	nonce := uint64(37)
	round := uint64(5)
	timestamp := uint64(1234)
	randSeed := []byte("a")
	epoch := uint32(7)
	hdr := &block.Header{
		Nonce:     nonce,
		Round:     round,
		TimeStamp: timestamp,
		RandSeed:  randSeed,
		Epoch:     epoch,
	}

	args := createMockBlockChainHookArgs()
	bh, _ := hooks.NewBlockChainHookImpl(args)

	bh.SetCurrentHeader(hdr)

	assert.Equal(t, nonce, bh.CurrentNonce())
	assert.Equal(t, round, bh.CurrentRound())
	assert.Equal(t, timestamp, bh.CurrentTimeStamp())
	assert.Equal(t, epoch, bh.CurrentEpoch())
	assert.Equal(t, randSeed, bh.CurrentRandomSeed())
}

func TestBlockChainHookImpl_IsPayableNormalAccount(t *testing.T) {
	t.Parallel()

	args := createMockBlockChainHookArgs()
	bh, _ := hooks.NewBlockChainHookImpl(args)
	isPayable, err := bh.IsPayable([]byte("address"), []byte("address"))
	assert.True(t, isPayable)
	assert.Nil(t, err)
}

func TestBlockChainHookImpl_IsPayableSCNonPayable(t *testing.T) {
	t.Parallel()

	args := createMockBlockChainHookArgs()
	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(address []byte) (vmcommon.AccountHandler, error) {
			acc := &mock.AccountWrapMock{}
			acc.SetCodeMetadata([]byte{0, 0})
			return acc, nil
		},
	}
	bh, _ := hooks.NewBlockChainHookImpl(args)
	isPayable, err := bh.IsPayable([]byte("address"), make([]byte, 32))
	assert.False(t, isPayable)
	assert.Nil(t, err)
}

func TestBlockChainHookImpl_IsPayablePayable(t *testing.T) {
	t.Parallel()

	args := createMockBlockChainHookArgs()
	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(address []byte) (handler vmcommon.AccountHandler, e error) {
			acc := &mock.AccountWrapMock{}
			acc.SetCodeMetadata([]byte{0, vmcommon.MetadataPayable})
			return acc, nil
		},
	}

	bh, _ := hooks.NewBlockChainHookImpl(args)
	isPayable, err := bh.IsPayable([]byte("address"), make([]byte, 32))
	assert.True(t, isPayable)
	assert.Nil(t, err)

	isPayable, err = bh.IsPayable(make([]byte, 32), make([]byte, 32))
	assert.True(t, isPayable)
	assert.Nil(t, err)
}

func TestBlockChainHookImpl_IsPayablePayableBySC(t *testing.T) {
	t.Parallel()

	args := createMockBlockChainHookArgs()
	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(address []byte) (handler vmcommon.AccountHandler, e error) {
			acc := &mock.AccountWrapMock{}
			acc.SetCodeMetadata([]byte{0, vmcommon.MetadataPayableBySC})
			return acc, nil
		},
	}

	bh, _ := hooks.NewBlockChainHookImpl(args)
	isPayable, err := bh.IsPayable(make([]byte, 32), make([]byte, 32))
	assert.True(t, isPayable)
	assert.Nil(t, err)
}

func TestBlockChainHookImpl_ProcessBuiltInFunction(t *testing.T) {
	t.Parallel()

	args := createMockBlockChainHookArgs()

	funcName := "func1"
	builtInFunctionsContainer := vmcommonBuiltInFunctions.NewBuiltInFunctionContainer()
	_ = builtInFunctionsContainer.Add(funcName, &mock.BuiltInFunctionStub{})
	args.BuiltInFunctions = builtInFunctionsContainer

	args.Accounts = &stateMock.AccountsStub{
		GetExistingAccountCalled: func(_ []byte) (vmcommon.AccountHandler, error) {
			return mock.NewAccountWrapMock([]byte("addr1")), nil
		},
		LoadAccountCalled: func(_ []byte) (vmcommon.AccountHandler, error) {
			return mock.NewAccountWrapMock([]byte("addr2")), nil
		},
	}

	bh, _ := hooks.NewBlockChainHookImpl(args)

	input := &vmcommon.ContractCallInput{
		Function: funcName,
	}
	output, err := bh.ProcessBuiltInFunction(input)
	require.NoError(t, err)
	require.Equal(t, vmcommon.Ok, output.ReturnCode)
}

func TestBlockChainHookImpl_GetESDTToken(t *testing.T) {
	t.Parallel()

	address := []byte("address")
	token := []byte("tkn")
	nonce := uint64(0)
	emptyESDTData := &esdt.ESDigitalToken{Value: big.NewInt(0)}
	expectedErr := errors.New("expected error")
	completeEsdtTokenKey := []byte(core.ElrondProtectedKeyPrefix + core.ESDTKeyIdentifier + string(token))
	testESDTData := &esdt.ESDigitalToken{
		Type:       uint32(core.Fungible),
		Value:      big.NewInt(1),
		Properties: []byte("properties"),
		TokenMetaData: &esdt.MetaData{
			Nonce:      1,
			Name:       []byte("name"),
			Creator:    []byte("creator"),
			Royalties:  2,
			Hash:       []byte("hash"),
			URIs:       [][]byte{[]byte("uri1"), []byte("uri2")},
			Attributes: []byte("attributes"),
		},
		Reserved: []byte("reserved"),
	}

	t.Run("account not found returns an empty esdt data", func(t *testing.T) {
		t.Parallel()

		args := createMockBlockChainHookArgs()
		args.Accounts = &stateMock.AccountsStub{
			GetExistingAccountCalled: func(_ []byte) (vmcommon.AccountHandler, error) {
				return nil, state.ErrAccNotFound
			},
		}

		bh, _ := hooks.NewBlockChainHookImpl(args)
		esdtData, err := bh.GetESDTToken(address, token, nonce)
		assert.Nil(t, err)
		require.NotNil(t, esdtData)
		assert.Equal(t, emptyESDTData, esdtData)
	})
	t.Run("accountsDB errors returns error", func(t *testing.T) {
		t.Parallel()

		args := createMockBlockChainHookArgs()
		args.Accounts = &stateMock.AccountsStub{
			GetExistingAccountCalled: func(_ []byte) (vmcommon.AccountHandler, error) {
				return nil, expectedErr
			},
		}

		bh, _ := hooks.NewBlockChainHookImpl(args)
		esdtData, err := bh.GetESDTToken(address, token, nonce)
		assert.Nil(t, esdtData)
		assert.Equal(t, expectedErr, err)
	})
	t.Run("backwards compatibility - retrieve value errors, should return error", func(t *testing.T) {
		t.Parallel()

		args := createMockBlockChainHookArgs()
		args.Accounts = &stateMock.AccountsStub{
			GetExistingAccountCalled: func(addres []byte) (vmcommon.AccountHandler, error) {
				addressHandler := mock.NewAccountWrapMock(address)
				addressHandler.SetDataTrie(nil)

				return addressHandler, nil
			},
		}

		bh, _ := hooks.NewBlockChainHookImpl(args)
		bh.SetFlagOptimizeNFTStore(false)

		esdtData, err := bh.GetESDTToken(address, token, nonce)
		assert.Nil(t, esdtData)
		assert.Equal(t, state.ErrNilTrie, err)
	})
	t.Run("backwards compatibility - empty byte slice should return empty esdt token", func(t *testing.T) {
		t.Parallel()

		args := createMockBlockChainHookArgs()
		args.Accounts = &stateMock.AccountsStub{
			GetExistingAccountCalled: func(addres []byte) (vmcommon.AccountHandler, error) {
				addressHandler := mock.NewAccountWrapMock(address)
				addressHandler.SetDataTrie(&trie.TrieStub{
					GetCalled: func(key []byte) ([]byte, error) {
						return make([]byte, 0), nil
					},
				})

				return addressHandler, nil
			},
		}

		bh, _ := hooks.NewBlockChainHookImpl(args)
		bh.SetFlagOptimizeNFTStore(false)

		esdtData, err := bh.GetESDTToken(address, token, nonce)
		assert.Equal(t, emptyESDTData, esdtData)
		assert.Nil(t, err)
	})
	t.Run("backwards compatibility - should load the esdt data in case of an NFT", func(t *testing.T) {
		t.Parallel()

		nftNonce := uint64(44)
		args := createMockBlockChainHookArgs()
		args.Accounts = &stateMock.AccountsStub{
			GetExistingAccountCalled: func(addres []byte) (vmcommon.AccountHandler, error) {
				addressHandler := mock.NewAccountWrapMock(address)
				buffToken, _ := args.Marshalizer.Marshal(testESDTData)
				key := append(completeEsdtTokenKey, big.NewInt(0).SetUint64(nftNonce).Bytes()...)
				_ = addressHandler.DataTrieTracker().SaveKeyValue(key, buffToken)

				return addressHandler, nil
			},
		}

		bh, _ := hooks.NewBlockChainHookImpl(args)
		bh.SetFlagOptimizeNFTStore(false)

		esdtData, err := bh.GetESDTToken(address, token, nftNonce)
		assert.Equal(t, testESDTData, esdtData)
		assert.Nil(t, err)
	})
	t.Run("backwards compatibility - should load the esdt data", func(t *testing.T) {
		t.Parallel()

		args := createMockBlockChainHookArgs()
		args.Accounts = &stateMock.AccountsStub{
			GetExistingAccountCalled: func(addres []byte) (vmcommon.AccountHandler, error) {
				addressHandler := mock.NewAccountWrapMock(address)
				buffToken, _ := args.Marshalizer.Marshal(testESDTData)
				_ = addressHandler.DataTrieTracker().SaveKeyValue(completeEsdtTokenKey, buffToken)

				return addressHandler, nil
			},
		}

		bh, _ := hooks.NewBlockChainHookImpl(args)
		bh.SetFlagOptimizeNFTStore(false)

		esdtData, err := bh.GetESDTToken(address, token, nonce)
		assert.Equal(t, testESDTData, esdtData)
		assert.Nil(t, err)
	})
	t.Run("new optimized implementation - NFTStorageHandler errors", func(t *testing.T) {
		t.Parallel()

		nftNonce := uint64(44)
		args := createMockBlockChainHookArgs()
		args.Accounts = &stateMock.AccountsStub{
			GetExistingAccountCalled: func(addres []byte) (vmcommon.AccountHandler, error) {
				return mock.NewAccountWrapMock(address), nil
			},
		}
		args.NFTStorageHandler = &testscommon.SimpleNFTStorageHandlerStub{
			GetESDTNFTTokenOnDestinationCalled: func(accnt vmcommon.UserAccountHandler, esdtTokenKey []byte, nonce uint64) (*esdt.ESDigitalToken, bool, error) {
				assert.Equal(t, completeEsdtTokenKey, esdtTokenKey)
				assert.Equal(t, nftNonce, nonce)

				return nil, false, expectedErr
			},
		}

		bh, _ := hooks.NewBlockChainHookImpl(args)

		esdtData, err := bh.GetESDTToken(address, token, nftNonce)
		assert.Nil(t, esdtData)
		assert.Equal(t, expectedErr, err)
	})
	t.Run("new optimized implementation - should return the esdt by calling NFTStorageHandler", func(t *testing.T) {
		t.Parallel()

		nftNonce := uint64(44)
		args := createMockBlockChainHookArgs()
		args.Accounts = &stateMock.AccountsStub{
			GetExistingAccountCalled: func(addres []byte) (vmcommon.AccountHandler, error) {
				return mock.NewAccountWrapMock(address), nil
			},
		}
		args.NFTStorageHandler = &testscommon.SimpleNFTStorageHandlerStub{
			GetESDTNFTTokenOnDestinationCalled: func(accnt vmcommon.UserAccountHandler, esdtTokenKey []byte, nonce uint64) (*esdt.ESDigitalToken, bool, error) {
				assert.Equal(t, completeEsdtTokenKey, esdtTokenKey)
				assert.Equal(t, nftNonce, nonce)
				copyToken := *testESDTData

				return &copyToken, false, nil
			},
		}

		bh, _ := hooks.NewBlockChainHookImpl(args)

		esdtData, err := bh.GetESDTToken(address, token, nftNonce)
		assert.Equal(t, testESDTData, esdtData)
		assert.Nil(t, err)
	})
}
