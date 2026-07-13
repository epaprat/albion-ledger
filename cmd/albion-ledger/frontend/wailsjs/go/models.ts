export namespace flow {
	
	export class CheckpointItem {
	    Kind: string;
	    Index: number;
	    Quality: number;
	    Qty: number;
	    LastSeen: number;
	
	    static createFrom(source: any = {}) {
	        return new CheckpointItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Kind = source["Kind"];
	        this.Index = source["Index"];
	        this.Quality = source["Quality"];
	        this.Qty = source["Qty"];
	        this.LastSeen = source["LastSeen"];
	    }
	}
	export class Checkpoint {
	    StartedMS: number;
	    LastActivityMS: number;
	    NetSilver: number;
	    LootValue: number;
	    GatherValue: number;
	    Fame: number;
	    UnvaluedCount: number;
	    EventCount: number;
	    Zone: string;
	    Items: CheckpointItem[];
	
	    static createFrom(source: any = {}) {
	        return new Checkpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.StartedMS = source["StartedMS"];
	        this.LastActivityMS = source["LastActivityMS"];
	        this.NetSilver = source["NetSilver"];
	        this.LootValue = source["LootValue"];
	        this.GatherValue = source["GatherValue"];
	        this.Fame = source["Fame"];
	        this.UnvaluedCount = source["UnvaluedCount"];
	        this.EventCount = source["EventCount"];
	        this.Zone = source["Zone"];
	        this.Items = this.convertValues(source["Items"], CheckpointItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace holdings {
	
	export class ContainerSnapshot {
	    GUID: string;
	    Location: string;
	    City: string;
	    Tab: string;
	    LastSeen: number;
	    Pinned: boolean;
	    Summary: boolean;
	    Items: model.HoldingItem[];
	
	    static createFrom(source: any = {}) {
	        return new ContainerSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.GUID = source["GUID"];
	        this.Location = source["Location"];
	        this.City = source["City"];
	        this.Tab = source["Tab"];
	        this.LastSeen = source["LastSeen"];
	        this.Pinned = source["Pinned"];
	        this.Summary = source["Summary"];
	        this.Items = this.convertValues(source["Items"], model.HoldingItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ItemCount {
	    Index: number;
	    Quality: number;
	    Count: number;
	
	    static createFrom(source: any = {}) {
	        return new ItemCount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Index = source["Index"];
	        this.Quality = source["Quality"];
	        this.Count = source["Count"];
	    }
	}
	export class ItemRef {
	    Index: number;
	    Quality: number;
	    Count: number;
	
	    static createFrom(source: any = {}) {
	        return new ItemRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Index = source["Index"];
	        this.Quality = source["Quality"];
	        this.Count = source["Count"];
	    }
	}
	export class ReconcileResult {
	    Match: boolean;
	    Extra: ItemCount[];
	    Missing: ItemCount[];
	    Report: string;
	
	    static createFrom(source: any = {}) {
	        return new ReconcileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Match = source["Match"];
	        this.Extra = this.convertValues(source["Extra"], ItemCount);
	        this.Missing = this.convertValues(source["Missing"], ItemCount);
	        this.Report = source["Report"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SlotItem {
	    ObjID: number;
	    Ref: ItemRef;
	
	    static createFrom(source: any = {}) {
	        return new SlotItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ObjID = source["ObjID"];
	        this.Ref = this.convertValues(source["Ref"], ItemRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace model {
	
	export class CaptureStatusView {
	    capturing: boolean;
	    interface: string;
	    gameServer?: string;
	    encryptedRate: number;
	    decoded: number;
	    driftAlert?: string;
	
	    static createFrom(source: any = {}) {
	        return new CaptureStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.capturing = source["capturing"];
	        this.interface = source["interface"];
	        this.gameServer = source["gameServer"];
	        this.encryptedRate = source["encryptedRate"];
	        this.decoded = source["decoded"];
	        this.driftAlert = source["driftAlert"];
	    }
	}
	export class MasteryLevel {
	    index: number;
	    name: string;
	    level: number;
	    progress: number;
	    fame: number;
	    category: string;
	    subcategory: string;
	    slot: string;
	    base: boolean;
	    touched: boolean;
	    fameToMax: number;
	
	    static createFrom(source: any = {}) {
	        return new MasteryLevel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.name = source["name"];
	        this.level = source["level"];
	        this.progress = source["progress"];
	        this.fame = source["fame"];
	        this.category = source["category"];
	        this.subcategory = source["subcategory"];
	        this.slot = source["slot"];
	        this.base = source["base"];
	        this.touched = source["touched"];
	        this.fameToMax = source["fameToMax"];
	    }
	}
	export class CharacterSpec {
	    masteries: MasteryLevel[];
	    nodeCount: number;
	    totalFame: number;
	    complete: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CharacterSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.masteries = this.convertValues(source["masteries"], MasteryLevel);
	        this.nodeCount = source["nodeCount"];
	        this.totalFame = source["totalFame"];
	        this.complete = source["complete"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SectionState {
	    seen: boolean;
	    lastSeen: number;
	    stale: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SectionState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seen = source["seen"];
	        this.lastSeen = source["lastSeen"];
	        this.stale = source["stale"];
	    }
	}
	export class TabSummary {
	    name: string;
	    itemCount: number;
	    subtotal: number;
	    unvaluedCount: number;
	    opened: boolean;
	    state: SectionState;
	
	    static createFrom(source: any = {}) {
	        return new TabSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.itemCount = source["itemCount"];
	        this.subtotal = source["subtotal"];
	        this.unvaluedCount = source["unvaluedCount"];
	        this.opened = source["opened"];
	        this.state = this.convertValues(source["state"], SectionState);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CitySummary {
	    name: string;
	    isInventory: boolean;
	    total: number;
	    unvaluedCount: number;
	    tabs: TabSummary[];
	    state: SectionState;
	    vaultValue: number;
	
	    static createFrom(source: any = {}) {
	        return new CitySummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.isInventory = source["isInventory"];
	        this.total = source["total"];
	        this.unvaluedCount = source["unvaluedCount"];
	        this.tabs = this.convertValues(source["tabs"], TabSummary);
	        this.state = this.convertValues(source["state"], SectionState);
	        this.vaultValue = source["vaultValue"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FlowEventView {
	    kind: string;
	    ts: number;
	    itemDisplayName?: string;
	    uniqueName?: string;
	    tier: number;
	    enchant: number;
	    quality: number;
	    count: number;
	    silver: number;
	    fame: number;
	    valued: boolean;
	    source?: string;
	    zone?: string;
	
	    static createFrom(source: any = {}) {
	        return new FlowEventView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.ts = source["ts"];
	        this.itemDisplayName = source["itemDisplayName"];
	        this.uniqueName = source["uniqueName"];
	        this.tier = source["tier"];
	        this.enchant = source["enchant"];
	        this.quality = source["quality"];
	        this.count = source["count"];
	        this.silver = source["silver"];
	        this.fame = source["fame"];
	        this.valued = source["valued"];
	        this.source = source["source"];
	        this.zone = source["zone"];
	    }
	}
	export class FlowItemStatView {
	    kind: string;
	    itemDisplayName: string;
	    uniqueName?: string;
	    tier: number;
	    enchant: number;
	    quality: number;
	    qty: number;
	    unitValue: number;
	    totalValue: number;
	    valued: boolean;
	    lastSeen: number;
	
	    static createFrom(source: any = {}) {
	        return new FlowItemStatView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.itemDisplayName = source["itemDisplayName"];
	        this.uniqueName = source["uniqueName"];
	        this.tier = source["tier"];
	        this.enchant = source["enchant"];
	        this.quality = source["quality"];
	        this.qty = source["qty"];
	        this.unitValue = source["unitValue"];
	        this.totalValue = source["totalValue"];
	        this.valued = source["valued"];
	        this.lastSeen = source["lastSeen"];
	    }
	}
	export class Valuation {
	    amount: number;
	    source: string;
	    asOf: number;
	    stale: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Valuation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.amount = source["amount"];
	        this.source = source["source"];
	        this.asOf = source["asOf"];
	        this.stale = source["stale"];
	    }
	}
	export class Item {
	    index: number;
	    displayName: string;
	    uniqueName?: string;
	    tier: number;
	    enchant: number;
	    quality: number;
	    known: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.displayName = source["displayName"];
	        this.uniqueName = source["uniqueName"];
	        this.tier = source["tier"];
	        this.enchant = source["enchant"];
	        this.quality = source["quality"];
	        this.known = source["known"];
	    }
	}
	export class HoldingItem {
	    objId: number;
	    item: Item;
	    valuation: Valuation;
	    location: string;
	    city: string;
	    group: string;
	    count: number;
	    lastSeen: number;
	
	    static createFrom(source: any = {}) {
	        return new HoldingItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.objId = source["objId"];
	        this.item = this.convertValues(source["item"], Item);
	        this.valuation = this.convertValues(source["valuation"], Valuation);
	        this.location = source["location"];
	        this.city = source["city"];
	        this.group = source["group"];
	        this.count = source["count"];
	        this.lastSeen = source["lastSeen"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HoldingsSummary {
	    totalValue: number;
	    walletSilver: number;
	    walletKnown: boolean;
	    walletLastSeen: number;
	    netWorth: number;
	    gameEstTotal: number;
	    unvaluedCount: number;
	    cities: CitySummary[];
	
	    static createFrom(source: any = {}) {
	        return new HoldingsSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalValue = source["totalValue"];
	        this.walletSilver = source["walletSilver"];
	        this.walletKnown = source["walletKnown"];
	        this.walletLastSeen = source["walletLastSeen"];
	        this.netWorth = source["netWorth"];
	        this.gameEstTotal = source["gameEstTotal"];
	        this.unvaluedCount = source["unvaluedCount"];
	        this.cities = this.convertValues(source["cities"], CitySummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class LiveViewItem {
	    item: Item;
	    valuation: Valuation;
	    lastSeen: number;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new LiveViewItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.item = this.convertValues(source["item"], Item);
	        this.valuation = this.convertValues(source["valuation"], Valuation);
	        this.lastSeen = source["lastSeen"];
	        this.count = source["count"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class SessionSummary {
	    selfKnown: boolean;
	    active: boolean;
	    startedMs: number;
	    elapsedMs: number;
	    netSilver: number;
	    silverPerHour: number;
	    lootValue: number;
	    gatherValue: number;
	    fame: number;
	    famePerHour: number;
	    rateReady: boolean;
	    unvaluedCount: number;
	    eventCount: number;
	    silverValue: number;
	    silverAvgPerHour: number;
	    lootAvgPerHour: number;
	    gatherAvgPerHour: number;
	    silverNowPerHour: number;
	    lootNowPerHour: number;
	    gatherNowPerHour: number;
	    fameNowPerHour: number;
	    nowPerHour: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.selfKnown = source["selfKnown"];
	        this.active = source["active"];
	        this.startedMs = source["startedMs"];
	        this.elapsedMs = source["elapsedMs"];
	        this.netSilver = source["netSilver"];
	        this.silverPerHour = source["silverPerHour"];
	        this.lootValue = source["lootValue"];
	        this.gatherValue = source["gatherValue"];
	        this.fame = source["fame"];
	        this.famePerHour = source["famePerHour"];
	        this.rateReady = source["rateReady"];
	        this.unvaluedCount = source["unvaluedCount"];
	        this.eventCount = source["eventCount"];
	        this.silverValue = source["silverValue"];
	        this.silverAvgPerHour = source["silverAvgPerHour"];
	        this.lootAvgPerHour = source["lootAvgPerHour"];
	        this.gatherAvgPerHour = source["gatherAvgPerHour"];
	        this.silverNowPerHour = source["silverNowPerHour"];
	        this.lootNowPerHour = source["lootNowPerHour"];
	        this.gatherNowPerHour = source["gatherNowPerHour"];
	        this.fameNowPerHour = source["fameNowPerHour"];
	        this.nowPerHour = source["nowPerHour"];
	    }
	}
	
	export class Trade {
	    tradeId: string;
	    direction: string;
	    source: string;
	    itemId: string;
	    itemName: string;
	    itemIndex: number;
	    partialAmount: number;
	    totalAmount: number;
	    gross: number;
	    setupFee: number;
	    salesTax: number;
	    net: number;
	    taxEstimated: boolean;
	    unitSilver: number;
	    received: number;
	    locationId: string;
	
	    static createFrom(source: any = {}) {
	        return new Trade(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tradeId = source["tradeId"];
	        this.direction = source["direction"];
	        this.source = source["source"];
	        this.itemId = source["itemId"];
	        this.itemName = source["itemName"];
	        this.itemIndex = source["itemIndex"];
	        this.partialAmount = source["partialAmount"];
	        this.totalAmount = source["totalAmount"];
	        this.gross = source["gross"];
	        this.setupFee = source["setupFee"];
	        this.salesTax = source["salesTax"];
	        this.net = source["net"];
	        this.taxEstimated = source["taxEstimated"];
	        this.unitSilver = source["unitSilver"];
	        this.received = source["received"];
	        this.locationId = source["locationId"];
	    }
	}
	export class TradeSummary {
	    grossIncome: number;
	    grossExpense: number;
	    salesTax: number;
	    setupFee: number;
	    net: number;
	    count: number;
	    scope: string;
	    windowStart: number;
	
	    static createFrom(source: any = {}) {
	        return new TradeSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.grossIncome = source["grossIncome"];
	        this.grossExpense = source["grossExpense"];
	        this.salesTax = source["salesTax"];
	        this.setupFee = source["setupFee"];
	        this.net = source["net"];
	        this.count = source["count"];
	        this.scope = source["scope"];
	        this.windowStart = source["windowStart"];
	    }
	}
	
	export class ZoneActivityStatView {
	    kind: string;
	    total: number;
	    perHour: number;
	    eventCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ZoneActivityStatView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.total = source["total"];
	        this.perHour = source["perHour"];
	        this.eventCount = source["eventCount"];
	    }
	}
	export class ZoneStatView {
	    zone: string;
	    activeMs: number;
	    netSilver: number;
	    silverPerHour: number;
	    gatherValue: number;
	    gatherPerHour: number;
	    fame: number;
	    famePerHour: number;
	    eventCount: number;
	    insufficientData: boolean;
	    activities: ZoneActivityStatView[];
	
	    static createFrom(source: any = {}) {
	        return new ZoneStatView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zone = source["zone"];
	        this.activeMs = source["activeMs"];
	        this.netSilver = source["netSilver"];
	        this.silverPerHour = source["silverPerHour"];
	        this.gatherValue = source["gatherValue"];
	        this.gatherPerHour = source["gatherPerHour"];
	        this.fame = source["fame"];
	        this.famePerHour = source["famePerHour"];
	        this.eventCount = source["eventCount"];
	        this.insufficientData = source["insufficientData"];
	        this.activities = this.convertValues(source["activities"], ZoneActivityStatView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace wailsadapter {
	
	export class ExportResult {
	    dataset: string;
	    path: string;
	    rows: number;
	    canceled: boolean;
	    err: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dataset = source["dataset"];
	        this.path = source["path"];
	        this.rows = source["rows"];
	        this.canceled = source["canceled"];
	        this.err = source["err"];
	    }
	}

}

