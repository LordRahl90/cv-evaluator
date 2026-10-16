// Score bands shared by rings, bars and badges. The API caps a CV that
// misses a must-have at 69, so "strong" starts at 70.
export type ScoreBand = 'high' | 'mid' | 'low'

export function scoreBand(score: number): ScoreBand {
  if (score >= 70) return 'high'
  if (score >= 50) return 'mid'
  return 'low'
}

export function scoreLabel(score: number): string {
  return { high: 'Strong match', mid: 'Partial match', low: 'Weak match' }[scoreBand(score)]
}
