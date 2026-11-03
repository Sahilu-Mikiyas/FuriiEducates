export const money = (minor: number, currency = 'ETB') => new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(minor / 100)
