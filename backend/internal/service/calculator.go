package service

import (
	"container/heap"
	"fgo-calc-backend/internal/model"
	"fgo-calc-backend/internal/repository"
	"log"
	"runtime"
	"sort"
	"sync"
	"time"
)

const OPTIMIZE_LIMIT = 5
const TEA_TIME_ID = 9403520

type CalculatorService struct {
	repo *repository.Repository
}

func NewCalculatorService(repo *repository.Repository) *CalculatorService {
	return &CalculatorService{repo: repo}
}

func (s *CalculatorService) FilterServants(traits []int, includeSvt []int, excludeSvt []int) []model.Servant {
	includeSet := map[int]bool{}
	excludeSet := map[int]bool{}
	for _, id := range includeSvt {
		includeSet[id] = true
	}
	for _, id := range excludeSvt {
		excludeSet[id] = true
	}

	result := []model.Servant{}
	traitSet := map[int]bool{}
	for _, t := range traits {
		traitSet[t] = true
	}

	servants := s.repo.GetServants()
	for _, svt := range servants {
		if excludeSet[svt.Id] {
			continue
		}
		if includeSet[svt.Id] {
			result = append(result, svt)
			continue
		}
		if len(traits) == 0 {
			result = append(result, svt)
			continue
		}

		matched := false
		for _, detail := range svt.Diff {
			for _, st := range detail.Traits {
				if traitSet[st] {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			result = append(result, svt)
		}
	}
	return result
}

func (s *CalculatorService) computeEffectsForCombo(ceCombo []model.CraftEssence, svtId int, diffKey string) (float64, int) {
	totalPercent := 0.0
	totalDirect := 0
	ceEffects := s.repo.GetCeEffects()
	for _, ce := range ceCombo {
		if m1, ok := ceEffects[ce.Id]; ok {
			if m2, ok2 := m1[svtId]; ok2 {
				if eff, ok3 := m2[diffKey]; ok3 {
					totalPercent += eff.Percent
					totalDirect += eff.Direct
				}
			}
		}
	}
	return totalPercent, totalDirect
}

func (s *CalculatorService) FindInPool(id int, cePool []model.CraftEssence, included []model.CraftEssence) bool {
	for _, ce := range cePool {
		if ce.Id == id {
			return true
		}
	}
	for _, ce := range included {
		if ce.Id == id {
			return true
		}
	}
	return false
}

func (s *CalculatorService) FixDominateMap(cePool []model.CraftEssence, included []model.CraftEssence) map[int]int {
	fixedMap := make(map[int]int)
	dominateMap := s.repo.GetDominateMap()
	for B, A := range dominateMap {
		if !s.FindInPool(B, cePool, included) {
			continue
		}
		currentA := A
		for {
			if !s.FindInPool(currentA, cePool, included) {
				if nextA, ok := dominateMap[currentA]; ok {
					currentA = nextA
				} else {
					currentA = -1
					break
				}
			} else {
				break
			}
		}
		if currentA != -1 {
			fixedMap[B] = currentA
		}
	}
	return fixedMap
}

func (s *CalculatorService) GetCombination(num int, includeCe []int, excludeCe []int) [][]model.CraftEssence {
	if num <= 0 {
		return [][]model.CraftEssence{}
	}
	includeSet := map[int]bool{}
	excludeSet := map[int]bool{}
	for _, id := range includeCe {
		includeSet[id] = true
	}
	for _, id := range excludeCe {
		excludeSet[id] = true
	}

	craftEssences := s.repo.GetCraftEssences()
	included := []model.CraftEssence{}
	pool := []model.CraftEssence{}
	for _, ce := range craftEssences {
		if excludeSet[ce.Id] {
			continue
		}
		if includeSet[ce.Id] {
			included = append(included, ce)
		} else {
			pool = append(pool, ce)
		}
	}

	if len(pool) < num-len(included) {
		return [][]model.CraftEssence{}
	}

	if len(included) > num {
		return [][]model.CraftEssence{}
	}
	need := num - len(included)
	if need == 0 {
		comb := make([]model.CraftEssence, len(included))
		copy(comb, included)
		return [][]model.CraftEssence{comb}
	}

	sort.Slice(pool, func(i, j int) bool {
		eff1 := 0.0
		if len(pool[i].Filters) > 0 {
			eff1 = pool[i].Filters[0].Effect
		}
		eff2 := 0.0
		if len(pool[j].Filters) > 0 {
			eff2 = pool[j].Filters[0].Effect
		}
		if eff1 != eff2 {
			return eff1 > eff2
		}
		return pool[i].Id < pool[j].Id
	})

	results := [][]model.CraftEssence{}
	fixedDominateMap := s.FixDominateMap(pool, included)
	initialPickedSet := make(map[int]bool)
	for _, ce := range included {
		initialPickedSet[ce.Id] = true
	}

	var dfs func(start int, picked []model.CraftEssence, pickedSet map[int]bool)
	dfs = func(start int, picked []model.CraftEssence, pickedSet map[int]bool) {
		if len(picked) == need {
			comb := make([]model.CraftEssence, 0, num)
			comb = append(comb, included...)
			comb = append(comb, picked...)
			results = append(results, comb)
			return
		}
		remainSlots := need - len(picked)
		for i := start; i <= len(pool)-remainSlots; i++ {
			ce := pool[i]
			if domA, ok := fixedDominateMap[ce.Id]; ok {
				if !pickedSet[domA] {
					continue
				}
			}
			pickedSet[ce.Id] = true
			dfs(i+1, append(picked, pool[i]), pickedSet)
			delete(pickedSet, ce.Id)
		}
	}
	dfs(0, []model.CraftEssence{}, initialPickedSet)
	return results
}

func (s *CalculatorService) calculateBestSupport(team *model.Team, baseBond int, supportLimit int) (int, []model.CraftEssence) {
	if supportLimit <= 0 {
		return 0, []model.CraftEssence{}
	}

	supportPool := []model.CraftEssence{}
	craftEssences := s.repo.GetCraftEssences()
	for _, ce := range craftEssences {
		if ce.Id == TEA_TIME_ID || ce.Id == 9401970 { // Tea Time or Lunchtime
			supportPool = append(supportPool, ce)
			continue
		}
		// Check for >= 20% bond effect
		for _, filter := range ce.Filters {
			if filter.Effect >= 20 {
				supportPool = append(supportPool, ce)
				break
			}
		}
	}

	bestBonus := 0
	bestCEs := []model.CraftEssence{}

	ceEffects := s.repo.GetCeEffects()

	// Helper to calculate bonus for a set of support CEs
	calcBonus := func(ces []model.CraftEssence) int {
		totalPercent := 0.0
		totalDirect := 0
		for _, ce := range ces {
			if ce.Id == TEA_TIME_ID {
				totalPercent += 15.0 * float64(len(team.Servants))
				continue
			}
			for i, svt := range team.Servants {
				diffKey := team.DiffChoice[i]
				if m1, ok := ceEffects[ce.Id]; ok {
					if m2, ok2 := m1[svt.Id]; ok2 {
						if eff, ok3 := m2[diffKey]; ok3 {
							totalPercent += eff.Percent
							totalDirect += eff.Direct
						}
					}
				}
			}
		}
		return int(float64(baseBond)*totalPercent/100.0) + totalDirect
	}

	if supportLimit == 1 {
		for _, ce := range supportPool {
			bonus := calcBonus([]model.CraftEssence{ce})
			if bonus > bestBonus {
				bestBonus = bonus
				bestCEs = []model.CraftEssence{ce}
			}
		}
	} else if supportLimit == 2 {
		// Allow duplicates, so nested loop over same pool
		for i := 0; i < len(supportPool); i++ {
			for j := i + 1; j < len(supportPool); j++ { // Optimization: order doesn't matter
				ces := []model.CraftEssence{supportPool[i], supportPool[j]}
				bonus := calcBonus(ces)
				if bonus > bestBonus {
					bestBonus = bonus
					bestCEs = ces
				}
			}
		}
	}

	return bestBonus, bestCEs
}

func (s *CalculatorService) getEventBonus(svt *model.Servant, serverType string) int {
	bonus := 0
	if list, ok := svt.EventBonuses[serverType]; ok {
		for _, b := range list {
			bonus += b.Bonus
		}
	}
	return bonus
}

func (s *CalculatorService) Optimize(costLimit int, svtLimit int, ceLimit int, supportLimit int, allowTraits []int, includeSvt []int, includeSvtDiff []string, excludeSvt []int, includeCe []int, excludeCe []int, baseBond int, serverType string, enableEventBonus bool) ([]model.TeamResponse, time.Duration) {
	startTime := time.Now()
	log.Println("Optimize called with costLimit:", costLimit, "svtLimit:", svtLimit, "ceLimit:", ceLimit)
	if len(includeSvt) > svtLimit {
		return []model.TeamResponse{}, 0
	}
	if len(includeCe) > ceLimit {
		return []model.TeamResponse{}, 0
	}

	mince := len(includeCe)
	if mince < 0 {
		mince = 0
	}
	mince += (costLimit - svtLimit*16) / 12

	if mince > ceLimit {
		mince = ceLimit
	}

	cePool := [][]model.CraftEssence{}
	for i := mince; i <= ceLimit; i++ {
		combs := s.GetCombination(i, includeCe, excludeCe)
		cePool = append(cePool, combs...)
	}

	log.Println("CE Pool: ", len(cePool))

	svtPool := s.FilterServants(allowTraits, includeSvt, excludeSvt)
	log.Println("Servant Pool: ", len(svtPool))

	includeSvtSet := map[int]bool{}
	for _, id := range includeSvt {
		includeSvtSet[id] = true
	}
	includeSvtDiffMap := make(map[int]string)
	for i, id := range includeSvt {
		if i < len(includeSvtDiff) {
			includeSvtDiffMap[id] = includeSvtDiff[i]
		}
	}

	numWorkers := runtime.GOMAXPROCS(0)
	ceJobs := make(chan []model.CraftEssence, len(cePool))
	resultsChan := make(chan []model.Team, len(cePool))
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for ceCombo := range ceJobs {
			localTeams := []model.Team{}
			ceCost := 0
			for _, ce := range ceCombo {
				ceCost += ce.Cost
			}
			if ceCost > costLimit {
				resultsChan <- localTeams
				continue
			}

			mandatoryBonuses := []model.SvtBonus{}
			currentSvtPool := make([]model.Servant, 0, len(svtPool))
			for i := range svtPool {
				svt := &svtPool[i]
				if includeSvtSet[svt.Id] {
					if diffKey, ok := includeSvtDiffMap[svt.Id]; ok {
						if detail, ok := svt.Diff[diffKey]; ok {
							totalPercent, totalDirect := s.computeEffectsForCombo(ceCombo, svt.Id, diffKey)
							if enableEventBonus {
								totalPercent += float64(s.getEventBonus(svt, serverType))
							}
							bonus := int(float64(baseBond)*totalPercent/100.0) + totalDirect + baseBond
							mandatoryBonuses = append(mandatoryBonuses, model.SvtBonus{
								Svt:     svt,
								DiffKey: diffKey,
								Bonus:   bonus,
								Cost:    detail.Cost,
							})
							continue
						}
					}
					totalPercent, totalDirect := s.computeEffectsForCombo(ceCombo, svt.Id, "default")
					if enableEventBonus {
						totalPercent += float64(s.getEventBonus(svt, serverType))
					}
					maxBonus := int(float64(baseBond)*totalPercent/100.0) + totalDirect + baseBond
					bestDiffKey := "default"
					bestCost := svt.Diff["default"].Cost
					for key, detail := range svt.Diff {
						totalPercent, totalDirect := s.computeEffectsForCombo(ceCombo, svt.Id, key)
						if enableEventBonus {
							totalPercent += float64(s.getEventBonus(svt, serverType))
						}
						currentBonus := int(float64(baseBond)*totalPercent/100.0) + totalDirect + baseBond
						if currentBonus > maxBonus {
							maxBonus = currentBonus
							bestDiffKey = key
							bestCost = detail.Cost
						}
					}
					mandatoryBonuses = append(mandatoryBonuses, model.SvtBonus{
						Svt:     svt,
						DiffKey: bestDiffKey,
						Bonus:   maxBonus,
						Cost:    bestCost,
					})
				} else {
					currentSvtPool = append(currentSvtPool, *svt)
				}
			}

			mandatoryCost := 0
			mandatoryBond := 0
			for _, mb := range mandatoryBonuses {
				mandatoryCost += mb.Cost
				mandatoryBond += mb.Bonus
			}

			currentCostLimit := costLimit - ceCost - mandatoryCost
			currentSvtLimit := svtLimit - len(mandatoryBonuses)
			if currentCostLimit < 0 || currentSvtLimit < 0 {
				resultsChan <- localTeams
				continue
			}

			if currentSvtLimit == 0 {
				team := model.Team{
					CraftEssences: ceCombo,
					TotalBond:     mandatoryBond,
					TotalCost:     ceCost + mandatoryCost,
				}
				for _, sb := range mandatoryBonuses {
					team.Servants = append(team.Servants, *sb.Svt)
					team.DiffChoice = append(team.DiffChoice, sb.DiffKey)
				}
				localTeams = append(localTeams, team)
				resultsChan <- localTeams
				continue
			}

			optionalBonuses := make([]model.SvtBonus, 0, len(currentSvtPool))
			for i := range currentSvtPool {
				svt := &currentSvtPool[i]
				totalPercent, totalDirect := s.computeEffectsForCombo(ceCombo, svt.Id, "default")
				if enableEventBonus {
					totalPercent += float64(s.getEventBonus(svt, serverType))
				}
				maxBonus := int(float64(baseBond)*totalPercent/100.0) + totalDirect + baseBond
				bestDiffKey := "default"
				bestCost := svt.Diff["default"].Cost
				for key, detail := range svt.Diff {
					if key == "default" {
						continue
					}
					totalPercent, totalDirect := s.computeEffectsForCombo(ceCombo, svt.Id, key)
					if enableEventBonus {
						totalPercent += float64(s.getEventBonus(svt, serverType))
					}
					currentBonus := int(float64(baseBond)*totalPercent/100.0) + totalDirect + baseBond
					if currentBonus > maxBonus {
						maxBonus = currentBonus
						bestDiffKey = key
						bestCost = detail.Cost
					}
				}
				optionalBonuses = append(optionalBonuses, model.SvtBonus{
					Svt:     svt,
					DiffKey: bestDiffKey,
					Bonus:   maxBonus,
					Cost:    bestCost,
				})
			}

			if len(optionalBonuses) == 0 {
				team := model.Team{
					CraftEssences: ceCombo,
					TotalBond:     mandatoryBond,
					TotalCost:     ceCost + mandatoryCost,
				}
				for _, sb := range mandatoryBonuses {
					team.Servants = append(team.Servants, *sb.Svt)
					team.DiffChoice = append(team.DiffChoice, sb.DiffKey)
				}
				localTeams = append(localTeams, team)
				resultsChan <- localTeams
				continue
			}

			const NEG = -1 << 60
			dp := make([][]int, currentSvtLimit+1)
			for i := range dp {
				dp[i] = make([]int, currentCostLimit+1)
				for j := range dp[i] {
					dp[i][j] = NEG
				}
			}
			dp[0][0] = 0

			paths := make([][]*model.PathNode, currentSvtLimit+1)
			for i := range paths {
				paths[i] = make([]*model.PathNode, currentCostLimit+1)
			}

			for itemIdx, item := range optionalBonuses {
				cost := item.Cost
				bonus := item.Bonus
				if cost > currentCostLimit {
					continue
				}
				for k := currentSvtLimit; k >= 1; k-- {
					for j := currentCostLimit; j >= cost; j-- {
						if dp[k-1][j-cost] == NEG {
							continue
						}
						newBond := dp[k-1][j-cost] + bonus
						if newBond > dp[k][j] {
							dp[k][j] = newBond
							paths[k][j] = &model.PathNode{
								ItemIdx: itemIdx,
								Prev:    paths[k-1][j-cost],
							}
						}
					}
				}
			}

			for k := 1; k <= currentSvtLimit; k++ {
				for j := 0; j <= currentCostLimit; j++ {
					if dp[k][j] == NEG {
						continue
					}
					used := make([]bool, len(optionalBonuses))
					node := paths[k][j]
					for node != nil {
						used[node.ItemIdx] = true
						node = node.Prev
					}
					chosen := []model.SvtBonus{}
					totalCost := ceCost + mandatoryCost
					for idx, flag := range used {
						if flag {
							sb := optionalBonuses[idx]
							chosen = append(chosen, sb)
							totalCost += sb.Cost
						}
					}

					team := model.Team{
						CraftEssences: ceCombo,
						TotalBond:     mandatoryBond,
					}
					for _, sb := range mandatoryBonuses {
						team.Servants = append(team.Servants, *sb.Svt)
						team.DiffChoice = append(team.DiffChoice, sb.DiffKey)
					}
					for _, sb := range chosen {
						team.Servants = append(team.Servants, *sb.Svt)
						team.DiffChoice = append(team.DiffChoice, sb.DiffKey)
						team.TotalBond += sb.Bonus
					}
					team.TotalCost = totalCost
					localTeams = append(localTeams, team)
				}
			}
			resultsChan <- localTeams
		}
	}

	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go worker()
	}

	go func() {
		for _, ceCombo := range cePool {
			ceJobs <- ceCombo
		}
		close(ceJobs)
	}()

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	deltaMax := 0
	if supportLimit > 0 {
		// DeltaMax = supportLimit * (MaxBond% - MinBond%) * BaseBond
		// MaxBond% = 20%, MinBond% = 15% (TeaTime) -> 5% diff
		deltaMax = int(float64(baseBond) * 0.05 * float64(svtLimit) * float64(supportLimit))
	}

	h := &model.TeamHeap{}
	heap.Init(h)
	// We need to store potentially optimal teams.
	// We will keep a larger set of teams in the heap, and then filter/re-rank at the end.
	// However, standard heap keeps smallest at top. We want to keep largest.
	// The standard logic was keeping TOP-N. Now we need "All within DeltaMax of Max".
	// Since we don't know the true global max until the end, we can track the current max seen so far.
	
	globalMaxBond := 0
	allCandidates := []model.Team{}

	for teams := range resultsChan {
		for _, team := range teams {
			if team.TotalBond > globalMaxBond {
				globalMaxBond = team.TotalBond
			}
			// Only keep teams that have a chance to beat current max + deltaMax logic?
			// Actually, we can just store them all if memory allows, or filter against "GlobalMax - DeltaMax"
			// But GlobalMax increases over time.
			// Let's store all candidates that are within reasonable range.
			// To be safe and since resultsChan is not sorted, we might need to buffer more.
			// Let's use a two-pass approach or just a simple list if not too huge.
			// Given FGO constraints, number of valid teams isn't infinite.
			// But to be safe, let's just use a slice and filter later.
			allCandidates = append(allCandidates, team)
		}
	}

	// Filter candidates based on final globalMaxBond
	finalCandidates := []model.Team{}
	threshold := globalMaxBond - deltaMax
	for _, team := range allCandidates {
		if team.TotalBond >= threshold {
			finalCandidates = append(finalCandidates, team)
		}
	}

	// Calculate support bonuses for final candidates
	for i := range finalCandidates {
		bonus, ces := s.calculateBestSupport(&finalCandidates[i], baseBond, supportLimit)
		finalCandidates[i].TotalBond += bonus
		finalCandidates[i].SupportCraftEssences = ces
	}

	// Re-build heap with final scores to get top N
	h = &model.TeamHeap{}
	heap.Init(h)
	for _, team := range finalCandidates {
		if h.Len() < OPTIMIZE_LIMIT {
			heap.Push(h, team)
		} else {
			top := (*h)[0]
			if team.TotalBond > top.TotalBond || (team.TotalBond == top.TotalBond && team.TotalCost > top.TotalCost) {
				(*h)[0] = team
				heap.Fix(h, 0)
			}
		}
	}

	limit := h.Len()
	sortedTeams := make([]model.Team, limit)
	for i := limit - 1; i >= 0; i-- {
		sortedTeams[i] = heap.Pop(h).(model.Team)
	}

	finalResults := make([]model.TeamResponse, 0, limit)
	ceEffects := s.repo.GetCeEffects()

	for i := 0; i < limit; i++ {
		team := sortedTeams[i]
		response := model.TeamResponse{
			Servants:            team.Servants,
			DiffChoice:          team.DiffChoice,
			TotalCost:           team.TotalCost,
			TotalBond:           team.TotalBond,
			CraftEssences:       make([]model.TeamResultCE, len(team.CraftEssences)),
			SupportCraftEssences: make([]model.TeamResultCE, len(team.SupportCraftEssences)),
		}

		for j, ce := range team.CraftEssences {
			totalContribution := 0
			for k, svt := range team.Servants {
				diffKey := team.DiffChoice[k]
				if m1, ok := ceEffects[ce.Id]; ok {
					if m2, ok2 := m1[svt.Id]; ok2 {
						if eff, ok3 := m2[diffKey]; ok3 {
							totalContribution += int(float64(baseBond)*eff.Percent/100.0) + eff.Direct
						}
					}
				}
			}
			response.CraftEssences[j] = model.TeamResultCE{
				CraftEssence: ce,
				Contribution: totalContribution,
			}
		}

		// Fill Support CE details in response
		for j, ce := range team.SupportCraftEssences {
			totalContribution := 0
			if ce.Id == TEA_TIME_ID {
				totalContribution = int(float64(baseBond) * 15.0 / 100.0 * float64(len(team.Servants)))
			} else {
				for k, svt := range team.Servants {
					diffKey := team.DiffChoice[k]
					if m1, ok := ceEffects[ce.Id]; ok {
						if m2, ok2 := m1[svt.Id]; ok2 {
							if eff, ok3 := m2[diffKey]; ok3 {
								totalContribution += int(float64(baseBond)*eff.Percent/100.0) + eff.Direct
							}
						}
					}
				}
			}
			response.SupportCraftEssences[j] = model.TeamResultCE{
				CraftEssence: ce,
				Contribution: totalContribution,
			}
		}

		finalResults = append(finalResults, response)
	}

	return finalResults, time.Since(startTime)
}

