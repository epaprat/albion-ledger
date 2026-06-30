export namespace holdings {
	
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
	        this.driftAlert = source["driftAlert"];
	    }
	}
	export class MasteryLevel {
	    index: number;
	    name: string;
	    level: number;
	
	    static createFrom(source: any = {}) {
	        return new MasteryLevel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.name = source["name"];
	        this.level = source["level"];
	    }
	}
	export class CharacterSpec {
	    masteries: MasteryLevel[];
	
	    static createFrom(source: any = {}) {
	        return new CharacterSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.masteries = this.convertValues(source["masteries"], MasteryLevel);
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
	    unvaluedCount: number;
	    cities: CitySummary[];
	
	    static createFrom(source: any = {}) {
	        return new HoldingsSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalValue = source["totalValue"];
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
	
	
	

}

