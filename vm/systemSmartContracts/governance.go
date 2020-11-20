//go:generate protoc -I=proto -I=$GOPATH/src -I=$GOPATH/src/github.com/gogo/protobuf/protobuf  --gogoslick_out=. governance.proto
package systemSmartContracts

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ElrondNetwork/elrond-go/config"
	"github.com/ElrondNetwork/elrond-go/core"
	"github.com/ElrondNetwork/elrond-go/core/atomic"
	"github.com/ElrondNetwork/elrond-go/core/check"
	"github.com/ElrondNetwork/elrond-go/hashing"
	"github.com/ElrondNetwork/elrond-go/marshal"
	"github.com/ElrondNetwork/elrond-go/vm"
	vmcommon "github.com/ElrondNetwork/elrond-vm-common"
)

const governanceConfigKey = "governanceConfig"
const hardForkPrefix = "hardFork"
const proposalPrefix = "proposal"
const accountLockPrefix = "accountLock"
const validatorLockPrefix = "validatorLock"
const whiteListPrefix = "whiteList"
const validatorPrefix = "validator"
const hardForkEpochGracePeriod = 2
const githubCommitLength = 40

// ArgsNewGovernanceContract defines the arguments needed for the on-chain governance contract
type ArgsNewGovernanceContract struct {
	Eei                        vm.SystemEI
	GasCost                    vm.GasCost
	GovernanceConfig           config.GovernanceSystemSCConfig
	GovernanceConfigV2         config.GovernanceSystemSCConfigV2
	Marshalizer                marshal.Marshalizer
	Hasher                     hashing.Hasher
	GovernanceSCAddress        []byte
	StakingSCAddress           []byte
	AuctionSCAddress           []byte
	InitalWhiteListedAddresses [][]byte
	EpochNotifier              vm.EpochNotifier
}

type governanceContract struct {
	eei                         vm.SystemEI
	gasCost                     vm.GasCost
	baseProposalCost            *big.Int
	ownerAddress                []byte
	governanceSCAddress         []byte
	stakingSCAddress            []byte
	auctionSCAddress            []byte
	marshalizer                 marshal.Marshalizer
	hasher                      hashing.Hasher
	governanceConfig            config.GovernanceSystemSCConfig
	governanceConfigV2          GovernanceConfigV2_0
	initialWhiteListedAddresses [][]byte
	enabledEpoch                uint32
	flagEnabled                 atomic.Flag
}

// NewGovernanceContract creates a new governance smart contract
func NewGovernanceContract(args ArgsNewGovernanceContract) (*governanceContract, error) {
	if check.IfNil(args.Eei) {
		return nil, vm.ErrNilSystemEnvironmentInterface
	}
	if check.IfNil(args.Marshalizer) {
		return nil, vm.ErrNilMarshalizer
	}
	if check.IfNil(args.Hasher) {
		return nil, vm.ErrNilHasher
	}
	if check.IfNil(args.EpochNotifier) {
		return nil, vm.ErrNilEpochNotifier
	}

	baseProposalCost, okConvert := big.NewInt(0).SetString(args.GovernanceConfig.ProposalCost, conversionBase)
	if !okConvert || baseProposalCost.Cmp(big.NewInt(0)) < 0 {
		return nil, vm.ErrInvalidBaseIssuingCost
	}

	g := &governanceContract{
		eei:                 args.Eei,
		gasCost:             args.GasCost,
		baseProposalCost:    baseProposalCost,
		ownerAddress:        nil,
		governanceSCAddress: args.GovernanceSCAddress,
		stakingSCAddress:    args.StakingSCAddress,
		auctionSCAddress:    args.AuctionSCAddress,
		marshalizer:         args.Marshalizer,
		hasher:              args.Hasher,
		governanceConfig:    args.GovernanceConfig,
		enabledEpoch:        args.GovernanceConfig.EnabledEpoch,
	}

	cfg, err := g.convertConfig(args.GovernanceConfigV2)
	if err != nil {
		return nil, err
	}
	g.governanceConfigV2 = *cfg

	err = g.validateInitialWhiteListedAddresses(args.InitalWhiteListedAddresses)
	if err != nil {
		return nil, err
	}
	g.initialWhiteListedAddresses = args.InitalWhiteListedAddresses

	args.EpochNotifier.RegisterNotifyHandler(g)

	return g, nil
}

// Execute calls one of the functions from the governance smart contract and runs the code according to the input
func (g *governanceContract) Execute(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if CheckIfNil(args) != nil {
		return vmcommon.UserError
	}

	if args.Function == core.SCDeployInitFunctionName {
		return g.init(args)
	}

	if !g.flagEnabled.IsSet() {
		g.eei.AddReturnMessage("Governance SC disabled")
		return vmcommon.UserError
	}

	switch args.Function {
	case "proposal":
		return g.proposal(args)
	case "vote":
		return g.vote(args)
	case "claimFunds":
		return g.claimFunds(args)
	case "whiteList":
		return g.whiteListProposal(args)
	case "hardFork":
		return g.hardForkProposal(args)
	case "changeConfig":
		return g.changeConfig(args)
	case "closeProposal":
		return g.closeProposal(args)
	case "getValidatorVotingPower":
		return g.getValidatorVotingPowerFromArgs(args)
	case "getBalanceVotingPower":
		return g.getBalanceVotingPower(args)
	}

	g.eei.AddReturnMessage("invalid method to call")
	return vmcommon.FunctionNotFound
}


func (g *governanceContract) init(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	scConfig := &GovernanceConfig{
		NumNodes:         g.governanceConfig.NumNodes,
		MinQuorum:        g.governanceConfig.MinQuorum,
		MinPassThreshold: g.governanceConfig.MinPassThreshold,
		MinVetoThreshold: g.governanceConfig.MinVetoThreshold,
		ProposalFee:      g.baseProposalCost,
	}
	marshaledData, err := g.marshalizer.Marshal(scConfig)
	log.LogIfError(err, "marshal error on governance init function")

	g.eei.SetStorage([]byte(governanceConfigKey), marshaledData)
	g.eei.SetStorage([]byte(ownerKey), args.CallerAddr)
	g.ownerAddress = make([]byte, 0, len(args.CallerAddr))
	g.ownerAddress = append(g.ownerAddress, args.CallerAddr...)
	return vmcommon.Ok
}

func (g *governanceContract) initV2(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	scConfig := &GovernanceConfigV2_0{
		MinQuorum:        g.governanceConfigV2.MinQuorum,
		MinPassThreshold: g.governanceConfigV2.MinPassThreshold,
		MinVetoThreshold: g.governanceConfigV2.MinVetoThreshold,
		ProposalFee:      g.baseProposalCost,
	}
	marshaledData, err := g.marshalizer.Marshal(scConfig)
	log.LogIfError(err, "marshal error on governance init function")

	g.eei.SetStorage([]byte(governanceConfigKey), marshaledData)
	g.eei.SetStorage([]byte(ownerKey), args.CallerAddr)
	g.ownerAddress = make([]byte, 0, len(args.CallerAddr))
	g.ownerAddress = append(g.ownerAddress, args.CallerAddr...)
	return vmcommon.Ok
}

// proposal creates a new proposal from passed arguments
func (g *governanceContract) proposal(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if args.CallValue.Cmp(g.baseProposalCost) != 0 {
		g.eei.AddReturnMessage("invalid proposal cost, expected " + g.baseProposalCost.String())
		return vmcommon.OutOfFunds
	}
	err := g.eei.UseGas(g.gasCost.MetaChainSystemSCsCost.Proposal)
	if err != nil {
		g.eei.AddReturnMessage("not enough gas")
		return vmcommon.OutOfGas
	}
	if len(args.Arguments) != 3 {
		g.eei.AddReturnMessage("invalid number of arguments, expected 3")
		return vmcommon.FunctionWrongSignature
	}
	if !g.isWhiteListed(args.CallerAddr) {
		g.eei.AddReturnMessage("called address is not whiteListed")
		return vmcommon.UserError
	}
	gitHubCommit := args.Arguments[0]
	if len(gitHubCommit) != githubCommitLength {
		g.eei.AddReturnMessage(fmt.Sprintf("invalid github commit length, wanted exactly %d", githubCommitLength))
		return vmcommon.UserError
	}
	if g.proposalExists(gitHubCommit) {
		g.eei.AddReturnMessage("proposal already exists")
		return vmcommon.UserError
	}

	startVoteNonce, endVoteNonce, err := g.startEndNonceFromArguments(args.Arguments[1], args.Arguments[2])
	if err != nil {
		g.eei.AddReturnMessage("invalid start/end vote nonce" + err.Error())
		return vmcommon.UserError
	}

	generalProposal := &GeneralProposalV2_0{
		IssuerAddress:  args.CallerAddr,
		GitHubCommit:   gitHubCommit,
		StartVoteNonce: startVoteNonce,
		EndVoteNonce:   endVoteNonce,
		Yes:            big.NewInt(0),
		No:             big.NewInt(0),
		Veto:           big.NewInt(0),
		Voted:          false,
		Votes:          make([][]byte, 0),
	}
	err = g.saveGeneralProposal(gitHubCommit, generalProposal)
	if err != nil {
		log.Warn("saveGeneralProposal", "err", err)
		g.eei.AddReturnMessage("saveGeneralProposal" + err.Error())
		return vmcommon.UserError
	}

	return vmcommon.Ok
}

// vote will cast a new vote
func (g *governanceContract) vote(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if args.CallValue.Cmp(zero) != 0 {
		return g.accountVote(args)
	}
	return g.validatorVote(args)
}

// accountVote casts a vote taking the transaction value as input for the vote power. It receives 2 arguments:
//  args.Arguments[0] - proposal reference (github commit)
//  args.Arguments[1] - vote option (yes, no, veto)
func (g *governanceContract) accountVote(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	err := g.eei.UseGas(g.gasCost.MetaChainSystemSCsCost.Vote)
	if err != nil {
		g.eei.AddReturnMessage("not enough gas")
		return vmcommon.OutOfGas
	}

	if len(args.Arguments) != 2 {
		g.eei.AddReturnMessage("invalid number of arguments, expected 2")
		return vmcommon.FunctionWrongSignature
	}

	voterAddress := args.CallerAddr
	proposalToVote := args.Arguments[0]
	proposal, err := g.getValidProposal(proposalToVote)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}
	voteOption, err := g.castVoteType(string(args.Arguments[1]))
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}

	votePower, err := g.computeVotingPower(args.CallValue)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}

	currentVoteData, err := g.getOrCreateVoteData(proposalToVote, voterAddress)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.ExecutionFailed
	}

	currentVote := &VoteDetails{
		Value:       voteOption,
		Power:       votePower,
		Balance:     args.CallValue,
		Type:        Account,
	}
	newVoteData, updatedProposal, err := g.applyVote(currentVote, currentVoteData, proposal)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}

	err = g.saveNewVoteData(voterAddress, newVoteData, updatedProposal, Account)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.ExecutionFailed
	}

	return vmcommon.Ok
}

// validatorVote casts a vote for a validator. This function can receive 3 or 4 parameters:
//  args.Arguments[0] - proposal reference (github commit)
//  args.Arguments[1] - vote option (yes, no, veto)
//  args.Arguments[2] - vote power used for this vote
//  args.Arguments[3] (optional) - an address that identifies if the vote was made on behalf of someone else - this
//   only helps for statistical and view purposes, it does not afftect the logic in any way
func (g *governanceContract) validatorVote(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	err := g.eei.UseGas(g.gasCost.MetaChainSystemSCsCost.Vote)
	if err != nil {
		g.eei.AddReturnMessage("not enough gas")
		return vmcommon.OutOfGas
	}

	if len(args.Arguments) < 3 || len(args.Arguments) > 4 {
		g.eei.AddReturnMessage("invalid number of arguments, expected 3 or 4")
		return vmcommon.FunctionWrongSignature
	}

	voterAddress := args.CallerAddr
	proposalToVote := args.Arguments[0]
	proposal, err := g.getValidProposal(proposalToVote)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}
	voteOption, err := g.castVoteType(string(args.Arguments[1]))
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}

	votePower := big.NewInt(0).SetBytes(args.Arguments[2])
	delegatedTo, err := g.getDelegatedToAddress(args)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}
	totalVotingPower, err := g.getValidatorVotingPower(voterAddress)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}

	currentVoteData, err := g.getOrCreateVoteData(proposalToVote, voterAddress)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.ExecutionFailed
	}
	if totalVotingPower.Cmp(big.NewInt(0).Add(votePower, currentVoteData.UsedPower)) == -1 {
		g.eei.AddReturnMessage("not enough voting power to cast this vote")
		return vmcommon.UserError
	}

	currentVote := &VoteDetails{
		Value:       voteOption,
		Power:       votePower,
		DelegatedTo: delegatedTo,
		Balance:     big.NewInt(0),
		Type:        Validator,
	}
	newVoteData, updatedProposal, err := g.applyVote(currentVote, currentVoteData, proposal)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}

	err = g.saveNewVoteData(voterAddress, newVoteData, updatedProposal, Validator)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.ExecutionFailed
	}

	return vmcommon.Ok
}

// claimFunds returns back the used funds for a particular proposal if they are unlocked. Accepts a single parameter:
//  args.Arguments[0] - proposal reference
func (g *governanceContract) claimFunds(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if args.CallValue.Cmp(big.NewInt(0)) != 0 {
		g.eei.AddReturnMessage("invalid callValue, should be 0")
		return vmcommon.UserError
	}

	if len(args.Arguments) != 1 {
		g.eei.AddReturnMessage("invalid number of arguments, expected 1")
		return vmcommon.FunctionWrongSignature
	}

	lock := g.getLock(args.CallerAddr, Account, args.Arguments[0])
	currentNonce := g.eei.BlockChainHook().CurrentNonce()

	if lock.Cmp(big.NewInt(0).SetUint64(currentNonce)) == -1 {
		g.eei.AddReturnMessage("your funds are still locked")
		return vmcommon.UserError
	}

	currentVoteData, err := g.getOrCreateVoteData(args.Arguments[0], args.CallerAddr)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.ExecutionFailed
	}
	if currentVoteData.Claimed == true {
		g.eei.AddReturnMessage("you already claimed back your funds")
		return vmcommon.UserError
	}

	fundsToTransfer := big.NewInt(0)
	for _, vote := range currentVoteData.VoteItems {
		if vote.Type != Account {
			continue
		}

		fundsToTransfer.Add(fundsToTransfer, vote.Balance)
	}

	if fundsToTransfer.Cmp(big.NewInt(0)) != 1 {
		g.eei.AddReturnMessage("no funds to claim for this proposal")
		return vmcommon.UserError
	}

	currentVoteData.Claimed = true
	err = g.saveVoteData(args.CallerAddr, currentVoteData, args.Arguments[0])
	if err != nil {
		g.eei.AddReturnMessage("could not save vote data as claimed")
		return vmcommon.ExecutionFailed
	}

	err = g.eei.Transfer(args.CallerAddr, g.governanceSCAddress, fundsToTransfer, nil, 0)
	if err != nil {
		g.eei.AddReturnMessage("transfer error on claimFunds function")
		return vmcommon.ExecutionFailed
	}

	return vmcommon.Ok
}

// whiteListProposal will create a new proposal to white list an address
func (g *governanceContract) whiteListProposal(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	currentNonce := g.eei.BlockChainHook().CurrentNonce()
	if currentNonce == 0 {
		return g.whiteListAtGenesis(args)
	}
	if args.CallValue.Cmp(g.baseProposalCost) != 0 {
		g.eei.AddReturnMessage("invalid callValue, needs exactly " + g.baseProposalCost.String())
		return vmcommon.OutOfFunds
	}
	err := g.eei.UseGas(g.gasCost.MetaChainSystemSCsCost.Proposal)
	if err != nil {
		g.eei.AddReturnMessage("not enough gas")
		return vmcommon.OutOfGas
	}
	if len(args.Arguments) != 3 {
		g.eei.AddReturnMessage("invalid number of arguments")
		return vmcommon.FunctionWrongSignature
	}
	if g.proposalExists(args.Arguments[0]) {
		g.eei.AddReturnMessage("cannot re-propose existing proposal")
		return vmcommon.UserError
	}
	if g.isWhiteListed(args.CallerAddr) {
		g.eei.AddReturnMessage("address is already whitelisted")
		return vmcommon.UserError
	}
	if len(args.Arguments[0]) != githubCommitLength {
		g.eei.AddReturnMessage(fmt.Sprintf("invalid github commit length, wanted exactly %d", githubCommitLength))
		return vmcommon.UserError
	}

	startVoteNonce, endVoteNonce, err := g.startEndNonceFromArguments(args.Arguments[1], args.Arguments[2])
	if err != nil {
		g.eei.AddReturnMessage("invalid start/end vote nonce " + err.Error())
		return vmcommon.UserError
	}

	key := append([]byte(proposalPrefix), args.CallerAddr...)
	whiteListAcc := &WhiteListProposal{
		WhiteListAddress: args.CallerAddr,
		ProposalStatus:   key,
	}

	key = append([]byte(whiteListPrefix), args.CallerAddr...)
	generalProposal := &GeneralProposalV2_0{
		IssuerAddress:  args.CallerAddr,
		GitHubCommit:   args.Arguments[0],
		StartVoteNonce: startVoteNonce,
		EndVoteNonce:   endVoteNonce,
		Yes:            big.NewInt(0),
		No:             big.NewInt(0),
		Veto:           big.NewInt(0),
		Voted:          false,
		Votes:          make([][]byte, 0),
	}

	marshaledData, err := g.marshalizer.Marshal(whiteListAcc)
	if err != nil {
		g.eei.AddReturnMessage("marshall error " + err.Error())
		return vmcommon.UserError
	}
	g.eei.SetStorage(key, marshaledData)

	err = g.saveGeneralProposal(args.CallerAddr, generalProposal)
	if err != nil {
		g.eei.AddReturnMessage("save proposal error " + err.Error())
		return vmcommon.UserError
	}

	return vmcommon.Ok
}

// hardForkProposal creates a new proposal for a hard-fork
func (g *governanceContract) hardForkProposal(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if args.CallValue.Cmp(g.baseProposalCost) != 0 {
		g.eei.AddReturnMessage("invalid proposal cost, expected " + g.baseProposalCost.String())
		return vmcommon.OutOfFunds
	}
	err := g.eei.UseGas(g.gasCost.MetaChainSystemSCsCost.Proposal)
	if err != nil {
		g.eei.AddReturnMessage("not enough gas")
		return vmcommon.OutOfGas
	}
	if len(args.Arguments) != 5 {
		g.eei.AddReturnMessage("invalid number of arguments, expected 5")
		return vmcommon.FunctionWrongSignature
	}
	if !g.isWhiteListed(args.CallerAddr) {
		g.eei.AddReturnMessage("called address is not whiteListed")
		return vmcommon.UserError
	}
	gitHubCommit := args.Arguments[2]
	if len(gitHubCommit) != githubCommitLength {
		g.eei.AddReturnMessage(fmt.Sprintf("invalid github commit length, wanted exactly %d", githubCommitLength))
		return vmcommon.UserError
	}
	if g.proposalExists(gitHubCommit) {
		g.eei.AddReturnMessage("proposal already exists")
		return vmcommon.UserError
	}

	key := append([]byte(hardForkPrefix), gitHubCommit...)
	marshaledData := g.eei.GetStorage(key)
	if len(marshaledData) != 0 {
		g.eei.AddReturnMessage("hardFork proposal already exists")
		return vmcommon.UserError
	}

	startVoteNonce, endVoteNonce, err := g.startEndNonceFromArguments(args.Arguments[3], args.Arguments[4])
	if err != nil {
		g.eei.AddReturnMessage("invalid start/end vote nonce" + err.Error())
		return vmcommon.UserError
	}

	bigIntEpochToHardFork, okConvert := big.NewInt(0).SetString(string(args.Arguments[0]), conversionBase)
	if !okConvert || !bigIntEpochToHardFork.IsUint64() {
		g.eei.AddReturnMessage("invalid argument for epoch")
		return vmcommon.UserError
	}

	epochToHardFork := uint32(bigIntEpochToHardFork.Uint64())
	currentEpoch := g.eei.BlockChainHook().CurrentEpoch()
	if epochToHardFork < currentEpoch && currentEpoch-epochToHardFork < hardForkEpochGracePeriod {
		g.eei.AddReturnMessage("invalid epoch to hardFork")
		return vmcommon.UserError
	}

	key = append([]byte(proposalPrefix), gitHubCommit...)
	hardForkProposal := &HardForkProposal{
		EpochToHardFork:    epochToHardFork,
		NewSoftwareVersion: args.Arguments[1],
		ProposalStatus:     key,
	}

	key = append([]byte(hardForkPrefix), gitHubCommit...)
	generalProposal := &GeneralProposalV2_0{
		IssuerAddress:  args.CallerAddr,
		GitHubCommit:   gitHubCommit,
		StartVoteNonce: startVoteNonce,
		EndVoteNonce:   endVoteNonce,
		Yes:            big.NewInt(0),
		No:             big.NewInt(0),
		Veto:           big.NewInt(0),
		Voted:          false,
		Votes:         make([][]byte, 0),
	}
	marshaledData, err = g.marshalizer.Marshal(hardForkProposal)
	if err != nil {
		log.Warn("hardFork proposal marshal", "err", err)
		g.eei.AddReturnMessage("marshal proposal" + err.Error())
		return vmcommon.UserError
	}
	g.eei.SetStorage(key, marshaledData)

	err = g.saveGeneralProposal(args.Arguments[0], generalProposal)
	if err != nil {
		log.Warn("save general proposal", "error", err)
		g.eei.AddReturnMessage("saveGeneralProposal" + err.Error())
		return vmcommon.UserError
	}

	return vmcommon.Ok
}

// changeConfig allows the owner to change the configuration for requesting proposals
func (g *governanceContract) changeConfig(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if !bytes.Equal(g.ownerAddress, args.CallerAddr) {
		g.eei.AddReturnMessage("changeConfig can be called only by owner")
		return vmcommon.UserError
	}
	if args.CallValue.Cmp(zero) != 0 {
		g.eei.AddReturnMessage("changeConfig can be called only without callValue")
		return vmcommon.UserError
	}
	if len(args.Arguments) != 4 {
		g.eei.AddReturnMessage("changeConfig needs 4 arguments")
		return vmcommon.UserError
	}

	numNodes, okConvert := big.NewInt(0).SetString(string(args.Arguments[0]), conversionBase)
	if !okConvert || numNodes.Cmp(big.NewInt(0)) < 0 {
		g.eei.AddReturnMessage("changeConfig first argument is incorrectly formatted")
		return vmcommon.UserError
	}
	minQuorum, okConvert := big.NewInt(0).SetString(string(args.Arguments[1]), conversionBase)
	if !okConvert || minQuorum.Cmp(big.NewInt(0)) < 0 {
		g.eei.AddReturnMessage("changeConfig second argument is incorrectly formatted")
		return vmcommon.UserError
	}
	minVeto, okConvert := big.NewInt(0).SetString(string(args.Arguments[2]), conversionBase)
	if !okConvert || minVeto.Cmp(big.NewInt(0)) < 0 {
		g.eei.AddReturnMessage("changeConfig third argument is incorrectly formatted")
		return vmcommon.UserError
	}
	minPass, okConvert := big.NewInt(0).SetString(string(args.Arguments[3]), conversionBase)
	if !okConvert || minPass.Cmp(big.NewInt(0)) < 0 {
		g.eei.AddReturnMessage("changeConfig fourth argument is incorrectly formatted")
		return vmcommon.UserError
	}

	scConfig, err := g.getConfig()
	if err != nil {
		g.eei.AddReturnMessage("changeConfig error " + err.Error())
		return vmcommon.UserError
	}

	scConfig.MinQuorum = minQuorum
	scConfig.MinVetoThreshold = minVeto
	scConfig.MinPassThreshold = minPass

	marshaledData, err := g.marshalizer.Marshal(scConfig)
	if err != nil {
		g.eei.AddReturnMessage("changeConfig error " + err.Error())
		return vmcommon.UserError
	}
	g.eei.SetStorage([]byte(governanceConfigKey), marshaledData)

	return vmcommon.Ok
}

// closeProposal generates and saves end results for a proposal
func (g *governanceContract) closeProposal(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if args.CallValue.Cmp(zero) != 0 {
		g.eei.AddReturnMessage("closeProposal callValue expected to be 0")
		return vmcommon.UserError
	}
	if !g.isWhiteListed(args.CallerAddr) {
		g.eei.AddReturnMessage("caller is not whitelisted")
		return vmcommon.UserError
	}
	if len(args.Arguments) != 1 {
		g.eei.AddReturnMessage("invalid number of arguments expected 1")
		return vmcommon.UserError
	}
	err := g.eei.UseGas(g.gasCost.MetaChainSystemSCsCost.CloseProposal)
	if err != nil {
		g.eei.AddReturnMessage("not enough gas")
		return vmcommon.OutOfGas
	}

	proposal := args.Arguments[0]
	generalProposal, err := g.getGeneralProposal(proposal)
	if err != nil {
		g.eei.AddReturnMessage("getGeneralProposal error " + err.Error())
		return vmcommon.UserError
	}
	if generalProposal.Closed {
		g.eei.AddReturnMessage("proposal is already closed, do nothing")
		return vmcommon.Ok
	}

	currentNonce := g.eei.BlockChainHook().CurrentNonce()
	if currentNonce < generalProposal.EndVoteNonce {
		g.eei.AddReturnMessage(fmt.Sprintf("proposal can be closed only after nonce %d", generalProposal.EndVoteNonce))
		return vmcommon.UserError
	}

	generalProposal.Closed = true
	err = g.computeEndResults(generalProposal)
	if err != nil {
		g.eei.AddReturnMessage("computeEndResults error" + err.Error())
		return vmcommon.UserError
	}

	err = g.saveGeneralProposal(proposal, generalProposal)
	if err != nil {
		g.eei.AddReturnMessage("saveGeneralProposal error" + err.Error())
		return vmcommon.UserError
	}

	for _, voter := range generalProposal.Votes {
		key := append(proposal, voter...)
		g.eei.SetStorage(key, nil)
	}

	return vmcommon.Ok
}

// getConfig returns the curent system smart contract configuration
func (g *governanceContract) getConfig() (*GovernanceConfigV2_0, error) {
	marshaledData := g.eei.GetStorage([]byte(governanceConfigKey))
	scConfig := &GovernanceConfigV2_0{}
	err := g.marshalizer.Unmarshal(scConfig, marshaledData)
	if err != nil {
		return nil, err
	}

	return scConfig, nil
}

// getValidatorVotingPowerFromArgs returns the total voting power for a validator. Un-staked nodes are not
//  taken into consideration
func (g *governanceContract) getValidatorVotingPowerFromArgs(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if args.CallValue.Cmp(zero) != 0 {
		g.eei.AddReturnMessage(vm.TransactionValueMustBeZero)
		return vmcommon.UserError
	}
	if len(args.Arguments) != 1 {
		g.eei.AddReturnMessage("function accepts only one argument, the validator address")
		return vmcommon.FunctionWrongSignature
	}
	validatorAddress := args.Arguments[0]
	if len(validatorAddress) != len(args.CallerAddr) {
		g.eei.AddReturnMessage("invalid argument - validator address")
		return vmcommon.UserError
	}

	votingPower, err := g.getValidatorVotingPower(validatorAddress)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.ExecutionFailed
	}

	g.eei.Finish(votingPower.Bytes())

	return vmcommon.Ok
}

// getBalanceVotingPower returns the voting power associated with the value sent in the transaction by the user
func (g *governanceContract) getBalanceVotingPower(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if args.CallValue.Cmp(zero) != 0 {
		g.eei.AddReturnMessage(vm.TransactionValueMustBeZero)
		return vmcommon.UserError
	}
	if len(args.Arguments) != 1 {
		g.eei.AddReturnMessage("function accepts only one argument, the balance for computing the power")
		return vmcommon.FunctionWrongSignature
	}

	balance := big.NewInt(0).SetBytes(args.Arguments[0])
	votingPower, err := g.computeVotingPower(balance)
	if err != nil {
		g.eei.AddReturnMessage(err.Error())
		return vmcommon.UserError
	}

	g.eei.Finish(votingPower.Bytes())
	return vmcommon.Ok
}

// saveGeneralProposal saves a proposal into the storage
func (g *governanceContract) saveGeneralProposal(reference []byte, generalProposal *GeneralProposalV2_0) error {
	marshaledData, err := g.marshalizer.Marshal(generalProposal)
	if err != nil {
		return err
	}
	key := append([]byte(proposalPrefix), reference...)
	g.eei.SetStorage(key, marshaledData)

	return nil
}

// getGeneralProposal returns a proposal from storage
func (g *governanceContract) getGeneralProposal(reference []byte) (*GeneralProposalV2_0, error) {
	key := append([]byte(proposalPrefix), reference...)
	marshaledData := g.eei.GetStorage(key)

	if len(marshaledData) == 0 {
		return nil, vm.ErrProposalNotFound
	}

	generalProposal := &GeneralProposalV2_0{}
	err := g.marshalizer.Unmarshal(generalProposal, marshaledData)
	if err != nil {
		return nil, err
	}

	return generalProposal, nil
}

// proposalExists returns true if a proposal already exists
func (g *governanceContract) proposalExists(reference []byte) bool {
	key := append([]byte(proposalPrefix), reference...)
	marshaledData := g.eei.GetStorage(key)
	return len(marshaledData) > 0
}

// getValidProposal returns a proposal from storage if it exists or it is still valid/in-progress
func (g *governanceContract) getValidProposal(reference []byte) (*GeneralProposalV2_0, error) {
	proposal, err := g.getGeneralProposal(reference)
	if err != nil {
		return nil, err
	}

	currentNonce := g.eei.BlockChainHook().CurrentNonce()
	if currentNonce < proposal.StartVoteNonce {
		return nil, vm.ErrVotingNotStartedForProposal
	}

	if currentNonce > proposal.EndVoteNonce {
		return nil, vm.ErrVotedForAnExpiredProposal
	}

	return proposal, nil
}

// isWhiteListed checks if an address is whitelisted
func (g *governanceContract) isWhiteListed(address []byte) bool {
	key := append([]byte(whiteListPrefix), address...)
	marshaledData := g.eei.GetStorage(key)
	if len(marshaledData) == 0 {
		return false
	}

	key = append([]byte(proposalPrefix), address...)
	marshaledData = g.eei.GetStorage(key)
	generalProposal := &GeneralProposal{}
	err := g.marshalizer.Unmarshal(generalProposal, marshaledData)
	if err != nil {
		return false
	}

	return generalProposal.Voted
}

func (g *governanceContract) whiteListAtGenesis(args *vmcommon.ContractCallInput) vmcommon.ReturnCode {
	if args.CallValue.Cmp(zero) != 0 {
		log.Warn("whiteList at genesis should be without callValue")
		return vmcommon.UserError
	}
	if g.isWhiteListed(args.CallerAddr) {
		log.Warn("address is already whiteListed")
		return vmcommon.UserError
	}
	if len(args.Arguments) != 0 {
		log.Warn("excepted argument number is 0")
		return vmcommon.UserError
	}
	if g.proposalExists(args.CallerAddr) {
		log.Warn("proposal with this key already exists")
		return vmcommon.UserError
	}

	key := append([]byte(proposalPrefix), args.CallerAddr...)
	whiteListAcc := &WhiteListProposal{
		WhiteListAddress: args.CallerAddr,
		ProposalStatus:   key,
	}

	key = append([]byte(whiteListPrefix), args.CallerAddr...)
	generalProposal := &GeneralProposalV2_0{
		IssuerAddress:  args.CallerAddr,
		GitHubCommit:   []byte("genesis"),
		StartVoteNonce: 0,
		EndVoteNonce:   0,
		Yes:            g.governanceConfigV2.MinQuorum,
		No:             big.NewInt(0),
		Veto:           big.NewInt(0),
		Voted:          true,
		Votes:          make([][]byte, 0),
	}
	marshaledData, err := g.marshalizer.Marshal(whiteListAcc)
	if err != nil {
		log.Warn("marshal error in whiteListAtGenesis", "err", err)
		return vmcommon.UserError
	}
	g.eei.SetStorage(key, marshaledData)

	err = g.saveGeneralProposal(args.CallerAddr, generalProposal)
	if err != nil {
		log.Warn("save general proposal ", "err", err)
		return vmcommon.UserError
	}

	return vmcommon.Ok
}

// applyVote takes in a vote and a full VoteData object and correctly applies the new vote, then returning
//  the new full VoteData object. In the same way applies the vote to the general proposal
func (g *governanceContract) applyVote(vote *VoteDetails, voteData *VoteDataV2_0, proposal *GeneralProposalV2_0) (*VoteDataV2_0, *GeneralProposalV2_0, error) {
	switch vote.Value {
	case Yes:
		voteData.TotalYes.Add(voteData.TotalYes, vote.Power)
		proposal.Yes.Add(proposal.Yes, vote.Power)
		break
	case No:
		voteData.TotalNo.Add(voteData.TotalNo, vote.Power)
		proposal.No.Add(proposal.No, vote.Power)
		break
	case Veto:
		voteData.TotalVeto.Add(voteData.TotalVeto, vote.Power)
		proposal.Veto.Add(proposal.Veto, vote.Power)
		break
	default:
		return nil, nil, fmt.Errorf("%s: %s", vm.ErrInvalidArgument, "invalid vote type")
	}

	voteData.UsedPower.Add(voteData.UsedPower, vote.Power)
	voteData.VoteItems = append(voteData.VoteItems, vote)

	return voteData, proposal, nil
}

// saveNewVoteData first saves the main vote data of the voter, then the full proposal with the updated information
func (g *governanceContract) saveNewVoteData(voter []byte, voteData *VoteDataV2_0, proposal *GeneralProposalV2_0, voteType VoteType) error {
	proposalKey := append([]byte(proposalPrefix), proposal.GitHubCommit...)
	voteItemKey := append(proposalKey, voter...)

	marshaledVoteItem, err := g.marshalizer.Marshal(voteData)
	if err != nil {
		return err
	}
	g.eei.SetStorage(voteItemKey, marshaledVoteItem)

	if !g.proposalContainsVoter(proposal, voteItemKey) {
		proposal.Votes = append(proposal.Votes, voteItemKey)
	}

	marshaledProposal, err := g.marshalizer.Marshal(proposal)
	g.eei.SetStorage(proposalKey, marshaledProposal)

	err = g.setLock(voter, voteType, proposal)
	if err != nil {
		return nil
	}

	return nil
}

// saveVoteData saves the provided vote data into the storage
func (g *governanceContract) saveVoteData(voter []byte, voteData *VoteDataV2_0, proposalReference []byte) error {
	proposalKey := append([]byte(proposalPrefix), proposalReference...)
	voteItemKey := append(proposalKey, voter...)

	marshaledVoteItem, err := g.marshalizer.Marshal(voteData)
	if err != nil {
		return err
	}
	g.eei.SetStorage(voteItemKey, marshaledVoteItem)

	return nil
}

// setLock will set a storage key with the nonce until the funds for a specific voter are locked
func (g *governanceContract) setLock(voter []byte, voteType VoteType, proposal *GeneralProposalV2_0) error {
	prefix := []byte(validatorLockPrefix)
	if voteType == Account {
		prefix = append([]byte(accountLockPrefix), proposal.GitHubCommit...)
	}
	lockKey := append(prefix, voter...)

	proposalDuration := proposal.EndVoteNonce - proposal.StartVoteNonce
	currentNonce := g.eei.BlockChainHook().CurrentNonce()
	lockNonce := big.NewInt(0).SetUint64(currentNonce + proposalDuration)

	g.eei.SetStorage(lockKey, lockNonce.Bytes())

	return nil
}

// getLock returns the lock nonce for a voter
func (g *governanceContract) getLock(voter []byte, voteType VoteType, proposalReferance []byte) *big.Int {
	prefix := []byte(validatorLockPrefix)
	if voteType == Account {
		prefix = append([]byte(accountLockPrefix), proposalReferance...)
	}
	lockKey := append(prefix, voter...)

	lock := g.eei.GetStorage(lockKey)

	return big.NewInt(0).SetBytes(lock)
}

// proposalContainsVoter iterates through all the votes on a proposal and returns if it already contains a
//  vote from a certain address
func (g *governanceContract) proposalContainsVoter(proposal *GeneralProposalV2_0, voteKey []byte) bool {
	for _, vote := range proposal.Votes {
		if bytes.Equal(vote, voteKey) {
			return true
		}
	}

	return false
}

// computeVotingPower returns the voting power for a value. The value can be either a balance or
//  the staked value for a validator
func (g *governanceContract) computeVotingPower(value *big.Int) (*big.Int, error) {
	if value.Cmp(big.NewInt(0)) == -1 {
		return nil, fmt.Errorf("cannot compute voting power on a negative value")
	}

	return big.NewInt(0).Sqrt(value), nil
}

// getDelegatedToAddress looks into the arguments passed and returns the optional delegatedTo address
func (g *governanceContract) getDelegatedToAddress(args *vmcommon.ContractCallInput) ([]byte, error) {
	if len(args.Arguments) < 4 {
		return make([]byte, 0), nil
	}
	if len(args.Arguments[3]) != len(args.CallerAddr) {
		return nil, fmt.Errorf("%s: %s", vm.ErrInvalidArgument, "invalid delegator address length")
	}

	return args.Arguments[2], nil
}

// isValidVoteString checks if a certain string represents a valid vote string
func (g *governanceContract) isValidVoteString(vote string) bool {
	switch vote {
	case "yes":
		return true
	case "no":
		return true
	case "veto":
		return true
	}
	return false
}

// castVoteType casts a valid string vote passed as an argument to the actual mapped value
func (g *governanceContract) castVoteType(vote string) (VoteValueType, error) {
	switch vote {
	case "yes":
		return Yes, nil
	case "no":
		return No, nil
	case "veto":
		return Veto, nil
		default: return 0, fmt.Errorf("%s: %s%s", vm.ErrInvalidArgument, "invalid vote type option: ", vote)
	}
}

// getOrCreateVoteData returns the vote data from storage for a goven proposer/validator pair.
//  If no vote data exists, it returns a new instance of VoteData
func (g *governanceContract) getOrCreateVoteData(proposal []byte, voter []byte) (*VoteDataV2_0, error) {
	key := append(proposal, voter...)
	marshaledData := g.eei.GetStorage(key)
	if len(marshaledData) == 0 {
		return g.getEmptyVoteData(), nil
	}

	voteData := &VoteDataV2_0{}
	err := g.marshalizer.Unmarshal(voteData, marshaledData)
	if err != nil {
		return nil, err
	}

	return voteData, nil
}

// getEmptyVoteData returns a new  VoteData instance with it's members initialised with their 0 value
func (g *governanceContract) getEmptyVoteData() *VoteDataV2_0 {
	return &VoteDataV2_0{
		UsedPower: big.NewInt(0),
		TotalYes: big.NewInt(0),
		TotalNo: big.NewInt(0),
		TotalVeto: big.NewInt(0),
		VoteItems: make([]*VoteDetails, 0),
	}
}

// getTotalStake returns the total stake for a given address. It does not
//  include values from nodes that were unstaked.
func (g *governanceContract) getTotalStake(address []byte) (*big.Int, error) {
	totalStake := big.NewInt(0)
	marshaledData := g.eei.GetStorageFromAddress(g.auctionSCAddress, address)
	if len(marshaledData) == 0 {
		return totalStake, nil
	}

	auctionData := &AuctionDataV2{}
	err := g.marshalizer.Unmarshal(auctionData, marshaledData)
	if err != nil {
		return totalStake, err
	}

	for _, blsKey := range auctionData.BlsPubKeys {
		marshaledData = g.eei.GetStorageFromAddress(g.stakingSCAddress, blsKey)
		if len(marshaledData) == 0 {
			continue
		}

		nodeData := &StakedDataV2_0{}
		err = g.marshalizer.Unmarshal(nodeData, marshaledData)
		if err != nil {
			return big.NewInt(0), err
		}

		if !nodeData.Staked {
			continue
		}

		totalStake.Add(totalStake, nodeData.StakeValue)
	}

	return totalStake, nil
}

// getValidatorVotingPower returns the total voting power of a validator
func (g *governanceContract) getValidatorVotingPower(validatorAddress []byte) (*big.Int, error) {
	totalStake, err := g.getTotalStake(validatorAddress)
	if err != nil {
		return nil, fmt.Errorf("could not return total stake for the provided address, thus cannot compute voting power")
	}

	votingPower, err := g.computeVotingPower(totalStake)
	if err != nil {
		return nil, fmt.Errorf("could not return total stake for the provided address, thus cannot compute voting power")
	}

	return votingPower, nil
}

// validateInitialWhiteListedAddresses makes basic checks that the provided initial whitelisted
//  addresses have the correct format
func (g *governanceContract) validateInitialWhiteListedAddresses(addresses [][]byte) error {
	if len(addresses) == 0 {
		log.Debug("0 initial whiteListed addresses provided to the governance contract")
		return nil
	}

	for _, addr := range addresses {
		if len(addr) != len(g.ownerAddress) {
			return fmt.Errorf("invalid address length for %s", string(addr))
		}
	}

	return nil
}

// startEndNonceFromArguments converts the nonce string arguments to uint64
func (g *governanceContract) startEndNonceFromArguments(argStart []byte, argEnd []byte) (uint64, uint64, error) {
	startVoteNonce, okConvert := big.NewInt(0).SetString(string(argStart), conversionBase)
	if !okConvert {
		return 0, 0, vm.ErrInvalidStartEndVoteNonce
	}
	if !startVoteNonce.IsUint64() {
		return 0, 0, vm.ErrInvalidStartEndVoteNonce
	}
	endVoteNonce, okConvert := big.NewInt(0).SetString(string(argEnd), conversionBase)
	if !okConvert {
		return 0, 0, vm.ErrInvalidStartEndVoteNonce
	}
	if !endVoteNonce.IsUint64() {
		return 0, 0, vm.ErrInvalidStartEndVoteNonce
	}
	currentNonce := g.eei.BlockChainHook().CurrentNonce()
	if currentNonce > startVoteNonce.Uint64() || startVoteNonce.Uint64() > endVoteNonce.Uint64() {
		return 0, 0, vm.ErrInvalidStartEndVoteNonce
	}

	return startVoteNonce.Uint64(), endVoteNonce.Uint64(), nil
}

// computeEndResults computes if a proposal has passed or not based on votes accumulated
func (g *governanceContract) computeEndResults(proposal *GeneralProposalV2_0) error {
	baseConfig, err := g.getConfig()
	if err != nil {
		return err
	}

	totalVotes := big.NewInt(0).Add(proposal.Yes, proposal.No)
	totalVotes.Add(totalVotes, proposal.Veto)

	if totalVotes.Cmp(baseConfig.MinQuorum) == -1 {
		proposal.Voted = false
		return nil
	}

	if proposal.Yes.Cmp(baseConfig.MinPassThreshold) == 1 && proposal.Yes.Cmp(proposal.No) == 1 {
		proposal.Voted = true
		return nil
	}

	if proposal.Veto.Cmp(baseConfig.MinVetoThreshold) == 1 {
		proposal.Voted = false
		return nil
	}

	return nil
}

// convertConfig converts the passed config file to the correct typed GovernanceConfig
func (g *governanceContract) convertConfig(config config.GovernanceSystemSCConfigV2) (*GovernanceConfigV2_0, error) {
	minQuorum, success := big.NewInt(0).SetString(config.MinQuorum, conversionBase)
	if !success {
		return nil, vm.ErrIncorrectConfig
	}
	minPass, success := big.NewInt(0).SetString(config.MinPassThreshold, conversionBase)
	if !success {
		return nil, vm.ErrIncorrectConfig
	}
	minVeto, success := big.NewInt(0).SetString(config.MinVetoThreshold, conversionBase)
	if !success {
		return nil, vm.ErrIncorrectConfig
	}
	proposalFee, success := big.NewInt(0).SetString(config.ProposalCost, conversionBase)
	if !success {
		return nil, vm.ErrIncorrectConfig
	}
	return &GovernanceConfigV2_0{
		MinQuorum: minQuorum,
		MinPassThreshold: minPass,
		MinVetoThreshold: minVeto,
		ProposalFee: proposalFee,
	}, nil
}

// EpochConfirmed is called whenever a new epoch is confirmed
func (g *governanceContract) EpochConfirmed(epoch uint32) {
	g.flagEnabled.Toggle(epoch >= g.enabledEpoch)
	log.Debug("governance contract", "enabled", g.flagEnabled.IsSet())
}

// CanUseContract returns true if contract is enabled
func (g *governanceContract) CanUseContract() bool {
	return true
}

// IsInterfaceNil returns true if underlying object is nil
func (g *governanceContract) IsInterfaceNil() bool {
	return g == nil
}
