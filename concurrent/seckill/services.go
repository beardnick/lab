package main

func CreateOrder(order Order) (id string, err error) {
	p := NewProductionDao()
	_, err = p.SubCnt(order.ProductionId, order.Cnt)
	if err != nil {
		return
	}
	id, err = NewOrderDao().Insert(order)
	if err != nil {
		return
	}
	return
}

func CreateProduction(production Production) (id string, err error) {
	p := NewProductionDao()
	id, err = p.Insert(production)
	if err != nil {
		return
	}
	err = p.CacheCnt(id, production.Cnt)
	return
}

func Check(productionId string) (result CheckResult, err error) {
	p := NewProductionDao()
	// todo 计数不对，先查库存，然后再减库存不是原子操作
	left, err := p.CacheCntOf(productionId)
	if err != nil {
		return
	}
	orderDao := NewOrderDao()
	ordered, err := orderDao.OrderSum(productionId)
	if err != nil {
		return
	}
	cnt, err := p.GetProductionCnt(productionId)
	if err != nil {
		return
	}
	result = CheckResult{
		Left:         left,
		Ordered:      ordered,
		CalCnt:       left + ordered,
		RealCnt:      cnt,
		ProductionId: productionId,
	}
	return
}
