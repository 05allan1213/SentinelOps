import { render, screen } from '@testing-library/react'
import { createElement } from 'react'

import ConfidenceBadge from '@/components/common/ConfidenceBadge'

describe('focused component test cycle', () => {
  it('renders a source component without global providers', () => {
    render(createElement(ConfidenceBadge, { confidence: 0.95 }))

    expect(screen.getByText('95%')).toBeInTheDocument()
  })
})
