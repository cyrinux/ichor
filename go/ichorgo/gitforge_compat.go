package ichorgo

import "github.com/cyrinux/ichor/go/gitforge"

// Aliases for the move of the forge transport to go/gitforge; removed in step 3, when the
// callers name gitforge directly. gitforge.BranchSuffix has no alias: the tests replace it,
// and an alias would be a copy their assignment does not reach.

type (
	gitForgeHost     = gitforge.Host
	gitRepoRef       = gitforge.RepoRef
	gitForge         = gitforge.Forge
	gitForgeEntry    = gitforge.Entry
	gitForgeTree     = gitforge.Tree
	gitForgeFile     = gitforge.File
	gitForgeCheck    = gitforge.CheckResult
	gitForgeRepoInfo = gitforge.RepoInfo
	gitRequestState  = gitforge.RequestState
	gitProposal      = gitforge.Proposal
	gitProposeResult = gitforge.ProposeResult
)

var (
	parseGitForgeHost   = gitforge.ParseHost
	newGitForge         = gitforge.New
	gitForgeArgs        = gitforge.Args
	gitForgeRepoArgs    = gitforge.RepoArgs
	gitForgeRepo        = gitforge.ParseRepoURL
	argoRepoWebURL      = gitforge.RepoWebURL
	sortGitForgeEntries = gitforge.SortEntries
	readGitForgeRepo    = gitforge.ReadRepo
	gitForgeRepoError   = gitforge.RepoError
	gitForgeArchive     = gitforge.Archive
	checkGitProposal    = gitforge.CheckProposal
	proposalPaths       = gitforge.ProposalPaths
	propose             = gitforge.Propose
	gitSlug             = gitforge.Slug
	gitBranchName       = gitforge.BranchName

	errGitForgeNotFound        = gitforge.ErrNotFound
	errGitForgeTooLarge        = gitforge.ErrTooLarge
	errGitForgeArchiveTooLarge = gitforge.ErrArchiveTooLarge
	errGitRequestNumber        = gitforge.ErrRequestNumber
)

const (
	gitForgeMaxFile        = gitforge.MaxFile
	gitForgeMaxArchive     = gitforge.MaxArchive
	gitForgeTimeout        = gitforge.Timeout
	gitProposeTimeout      = gitforge.ProposeTimeout
	gitForgeArchiveTimeout = gitforge.ArchiveTimeout

	gitForgeGitHub = gitforge.KindGitHub
	gitForgeGitLab = gitforge.KindGitLab
	gitForgeGitea  = gitforge.KindGitea
	gitForgeDemo   = gitforge.KindDemo

	gitForgeMaxBody    = gitforge.MaxBody
	gitProposeMaxFiles = gitforge.ProposeMaxFiles

	gitRequestOpen   = gitforge.RequestOpen
	gitRequestMerged = gitforge.RequestMerged
	gitRequestClosed = gitforge.RequestClosed
)
