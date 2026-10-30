package main

// seedProduct is one catalogue entry.
//
// Prices are given in naira here and converted to kobo on insert so the
// source data stays readable and auditable. Stock is always a positive
// opening quantity: the client's 2026-10-25 brief requires every product to
// be in stock.
type seedProduct struct {
	Name        string
	Category    string
	Unit        string
	PriceNaira  int64
	Stock       int
	Description string
	Image       string
	Featured    bool
}

// catalogue is the farm's full product range as of the 2026-10-25 brief,
// which replaced the prototype's fifteen products. The image file names match
// the assets copied into each frontend's public/images folder. One product
// per category carries Featured so the shop's "Featured" sort has an order;
// the home page's featured grid now rotates one random product per category
// and no longer reads this flag.
func catalogue() []seedProduct {
	return []seedProduct{
		// ===== Poultry =====
		{
			Name: "Broiler Chicken", Category: "poultry", Unit: "bird", PriceNaira: 6_500, Stock: 350,
			Description: "Healthy, fast-growing broiler chickens raised for quality meat production, well-fed and disease-monitored on our poultry unit.",
			Image:       "Broilers.jpeg", Featured: true,
		},
		{
			Name: "Noiler Chicken", Category: "poultry", Unit: "bird", PriceNaira: 7_000, Stock: 220,
			Description: "Dual-purpose Noiler birds valued for both eggs and meat, raised under a semi-intensive management system.",
			Image:       "Noilers.jpeg",
		},
		{
			Name: "Guinea Fowl", Category: "poultry", Unit: "bird", PriceNaira: 9_500, Stock: 60,
			Description: "Free-range guinea fowls known for their lean, flavourful meat, a delicacy in many Nigerian households.",
			Image:       "Guinea_fowls.jpeg",
		},
		{
			Name: "Duck", Category: "poultry", Unit: "bird", PriceNaira: 8_500, Stock: 40,
			Description: "Farm-raised ducks reared in a clean, well-watered environment, sold live and ready for pick-up or delivery.",
			Image:       "Duck.jpeg",
		},
		{
			Name: "Layers", Category: "poultry", Unit: "bird", PriceNaira: 5_500, Stock: 180,
			Description: "Point-of-lay and laying hens raised for consistent, quality egg production on a managed feeding programme.",
			Image:       "Layers.jpeg",
		},
		{
			Name: "Turkey", Category: "poultry", Unit: "bird", PriceNaira: 25_000, Stock: 55,
			Description: "Well-grown turkeys raised for premium meat, ideal for festive and household purchase.",
			Image:       "Turkeys.jpeg",
		},
		{
			Name: "Brahma Chicken", Category: "poultry", Unit: "bird", PriceNaira: 18_000, Stock: 30,
			Description: "Large, hardy Brahma birds prized for their size, gentle temperament and dual egg-and-meat value.",
			Image:       "Brahma.jpeg",
		},
		{
			Name: "Native (Local) Chicken", Category: "poultry", Unit: "bird", PriceNaira: 7_500, Stock: 90,
			Description: "Traditional free-range local chickens, slow-grown for the rich taste associated with native breeds.",
			Image:       "Native_fowl.jpeg",
		},

		// ===== Livestock =====
		{
			Name: "Cow (Cattle)", Category: "livestock", Unit: "head", PriceNaira: 850_000, Stock: 12,
			Description: "Mature, well-nourished cattle suitable for breeding or beef production, reared under our ruminant farming programme.",
			Image:       "cows.jpeg", Featured: true,
		},
		{
			Name: "Goat", Category: "livestock", Unit: "head", PriceNaira: 65_000, Stock: 40,
			Description: "Healthy, vaccinated goats raised on natural pasture and supplementary feed for optimum growth.",
			Image:       "Goat.jpeg",
		},
		{
			Name: "Sheep", Category: "livestock", Unit: "head", PriceNaira: 70_000, Stock: 35,
			Description: "Well-bred sheep reared under our ruminant unit, suitable for restocking or festive purchase.",
			Image:       "Sheep.jpeg",
		},
		{
			Name: "Ram", Category: "livestock", Unit: "head", PriceNaira: 120_000, Stock: 20,
			Description: "Prime, healthy rams carefully selected for size and condition, available for breeding and festive seasons.",
			Image:       "Ram.jpeg",
		},
		{
			Name: "Pig", Category: "livestock", Unit: "head", PriceNaira: 95_000, Stock: 25,
			Description: "Farm-raised pigs from our piggery unit, managed under strict hygiene and feeding protocols.",
			Image:       "pig.jpeg",
		},

		// ===== Micro-Livestock =====
		{
			Name: "Guinea Pig", Category: "micro-livestock", Unit: "animal", PriceNaira: 5_000, Stock: 60,
			Description: "Healthy guinea pigs reared for breeding stock and small-scale micro-livestock keeping.",
			Image:       "Guinea_pigs.jpeg",
		},
		{
			Name: "Rabbit", Category: "micro-livestock", Unit: "animal", PriceNaira: 8_000, Stock: 70,
			Description: "Well-fed rabbits raised for lean meat and breeding, a fast-multiplying micro-livestock enterprise.",
			Image:       "Rabbits.jpeg", Featured: true,
		},
		{
			Name: "Grasscutter", Category: "micro-livestock", Unit: "animal", PriceNaira: 15_000, Stock: 45,
			Description: "Domesticated grasscutters reared for their prized bushmeat and for breeding stock.",
			Image:       "Grasscutters.jpeg",
		},

		// ===== Crop Farming =====
		{
			Name: "Maize (Corn)", Category: "crops", Unit: "100kg bag", PriceNaira: 38_000, Stock: 80,
			Description: "Well-dried, quality maize grains harvested from our crop farm, suitable for milling, feed or resale.",
			Image:       "Maize.jpeg", Featured: true,
		},
		{
			Name: "Groundnut", Category: "crops", Unit: "100kg bag", PriceNaira: 55_000, Stock: 50,
			Description: "Quality groundnuts harvested and dried for oil extraction, snacks or further processing.",
			Image:       "Groundnut.jpeg",
		},
		{
			Name: "Rice (Paddy)", Category: "crops", Unit: "50kg bag", PriceNaira: 45_000, Stock: 60,
			Description: "Locally grown paddy rice from our crop division, cleaned and bagged for the local market.",
			Image:       "Rice.jpeg",
		},
		{
			Name: "Guinea Corn (Sorghum)", Category: "crops", Unit: "100kg bag", PriceNaira: 42_000, Stock: 55,
			Description: "Clean, sun-dried guinea corn (sorghum), a staple grain suitable for food processing and animal feed.",
			Image:       "Gunine_corn.jpeg",
		},

		{
			Name: "Snail", Category: "micro-livestock", Unit: "bag", PriceNaira: 25_000, Stock: 40,
			Description: "Fresh, plump snails, sustainably reared in our snailry and sold by the bag.",
			Image:       "Giant_Ghana_Tiger_Snail.jpeg",
		},

		// ===== Animal Nutrition =====
		{
			Name: "Broiler Growth Booster (1kg)", Category: "animal-nutrition", Unit: "pack", PriceNaira: 6500, Stock: 100,
			Description: "Mixed feed additive (Bacillus subtilis + Bacillus licheniformis) that supports gut health, faster growth and lower feed cost in broilers.",
			Image:       "nutrition-1.jpeg", Featured: true,
		},
		{
			Name: "Pig Growth Booster (1kg)", Category: "animal-nutrition", Unit: "pack", PriceNaira: 6500, Stock: 100,
			Description: "Bacillus subtilis and Saccharomyces cerevisiae feed additive that supports healthy digestion, steady weight gain and better feed conversion in pigs.",
			Image:       "nutrition-2.jpeg",
		},
		{
			Name: "BactoClear (100g)", Category: "animal-nutrition", Unit: "pack", PriceNaira: 3500, Stock: 100,
			Description: "Lysozyme and glycine feed additive that supports gut health and immunity, helping control diarrhoea and enteritis in livestock and poultry.",
			Image:       "nutrition-3.jpeg",
		},
		{
			Name: "Turkey Weight Gain Booster (1kg)", Category: "animal-nutrition", Unit: "pack", PriceNaira: 6500, Stock: 100,
			Description: "Feed additive that supports faster, healthier weight gain in turkeys.",
			Image:       "nutrition-5.jpeg",
		},
		{
			Name: "Vibrio Clear (100g)", Category: "animal-nutrition", Unit: "pack", PriceNaira: 3500, Stock: 100,
			Description: "Lysozyme and glycine feed additive for aquaculture that helps eliminate pathogenic Vibrio and supports healthy, thriving fish and shrimp.",
			Image:       "nutrition-6.jpeg",
		},
		{
			Name: "Ruminant Growth Booster (1kg)", Category: "animal-nutrition", Unit: "pack", PriceNaira: 6500, Stock: 100,
			Description: "Bacillus subtilis and Saccharomyces cerevisiae feed additive for cattle and sheep, improving roughage digestibility, weight gain and feed efficiency.",
			Image:       "nutrition-7.jpeg",
		},
		{
			Name: "Multivitamin Complex (1kg)", Category: "animal-nutrition", Unit: "pack", PriceNaira: 6500, Stock: 100,
			Description: "Taurine and L-ascorbic acid (vitamin C) multivitamin feed additive that boosts immunity, stress resistance and daily weight gain across livestock and poultry.",
			Image:       "nutrition-8.jpeg",
		},

		// ===== Spices — AROMACRAFT (100g) =====
		{
			Name: "AROMACRAFT Crushed Cayenne Pepper (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "AROMACRAFT crushed cayenne pepper, 100g — fiery, coarse flakes for stews, grills and sauces.",
			Image:       "spice-aromacraft-crushed-cayenne-pepper.jpeg", Featured: true,
		},
		{
			Name: "AROMACRAFT Ginger (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_000, Stock: 200,
			Description: "AROMACRAFT dried ground ginger, 100g — warm and aromatic for cooking, baking and teas.",
			Image:       "spice-aromacraft-ginger.jpeg",
		},
		{
			Name: "AROMACRAFT Pepper Soup Spice (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "AROMACRAFT pepper soup spice blend, 100g — the classic aromatic mix for Nigerian pepper soups.",
			Image:       "spice-aromacraft-pepper-soup-spice.jpeg",
		},
		{
			Name: "AROMACRAFT Turmeric (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_000, Stock: 200,
			Description: "AROMACRAFT ground turmeric, 100g — earthy, golden spice for cooking and wellness.",
			Image:       "spice-aromacraft-turmeric.jpeg",
		},
		{
			Name: "AROMACRAFT Black Pepper (100g)", Category: "spices", Unit: "pack", PriceNaira: 3_000, Stock: 200,
			Description: "AROMACRAFT ground black pepper, 100g — bold, pungent seasoning for everyday cooking.",
			Image:       "spice-aromacraft-black-pepper.jpeg",
		},
		{
			Name: "AROMACRAFT All Purpose Spice (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "AROMACRAFT all-purpose seasoning, 100g — a balanced everyday blend for meats, soups and rice.",
			Image:       "spice-aromacraft-all-purpose-spice.jpeg",
		},
		{
			Name: "AROMACRAFT Stew & Jollof Rice Spice (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "AROMACRAFT stew and jollof rice spice, 100g — a ready blend for rich tomato stews and jollof.",
			Image:       "spice-aromacraft-stew-jollof-rice-spice.jpeg",
		},
		{
			Name: "AROMACRAFT Ground Cayenne Pepper (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "AROMACRAFT finely ground cayenne pepper, 100g — even heat that blends smoothly into any dish.",
			Image:       "spice-aromacraft-ground-cayenne-pepper.jpeg",
		},
		{
			Name: "AROMACRAFT Curry Powder (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_000, Stock: 200,
			Description: "AROMACRAFT curry powder, 100g — a fragrant, well-rounded curry blend for stews and rice.",
			Image:       "spice-aromacraft-curry-powder.jpeg",
		},
		{
			Name: "AROMACRAFT Cinnamon Powder (100g)", Category: "spices", Unit: "pack", PriceNaira: 3_000, Stock: 200,
			Description: "AROMACRAFT ground cinnamon, 100g — sweet, warming spice for baking, drinks and desserts.",
			Image:       "spice-aromacraft-cinnamon-powder.jpeg",
		},
		{
			Name: "AROMACRAFT White Pepper (100g)", Category: "spices", Unit: "pack", PriceNaira: 3_000, Stock: 200,
			Description: "AROMACRAFT ground white pepper, 100g — mild, earthy heat for light sauces and soups.",
			Image:       "spice-aromacraft-white-pepper.jpeg",
		},

		// ===== Spices — ROYAL LUSH (100g) =====
		{
			Name: "ROYAL LUSH All Purpose Spice (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "ROYAL LUSH all-purpose seasoning, 100g — a versatile blend for meat, fish, soups and rice.",
			Image:       "spice-royal-lush-all-purpose-spice.jpeg",
		},
		{
			Name: "ROYAL LUSH Stew & Jollof Rice Spice (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "ROYAL LUSH stew and jollof rice spice, 100g — a rich blend for tomato stews and party jollof.",
			Image:       "spice-royal-lush-stew-jollof-rice-spice.jpeg",
		},
		{
			Name: "ROYAL LUSH Yaji (Suya Spice) (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "ROYAL LUSH yaji, 100g — the classic peppery, nutty suya spice for grilled meat and snacks.",
			Image:       "spice-royal-lush-yaji-suya-spice.jpeg",
		},
		{
			Name: "ROYAL LUSH Cinnamon Powder (100g)", Category: "spices", Unit: "pack", PriceNaira: 3_000, Stock: 200,
			Description: "ROYAL LUSH ground cinnamon, 100g — fragrant and sweet for baking, drinks and oatmeal.",
			Image:       "spice-royal-lush-cinnamon-powder.jpeg",
		},
		{
			Name: "ROYAL LUSH Turmeric (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_000, Stock: 200,
			Description: "ROYAL LUSH ground turmeric, 100g — golden, earthy spice for cooking and wellness.",
			Image:       "spice-royal-lush-turmeric.jpeg",
		},
		{
			Name: "ROYAL LUSH Black Pepper (100g)", Category: "spices", Unit: "pack", PriceNaira: 3_000, Stock: 200,
			Description: "ROYAL LUSH ground black pepper, 100g — bold, aromatic seasoning for everyday dishes.",
			Image:       "spice-royal-lush-black-pepper.jpeg",
		},
		{
			Name: "ROYAL LUSH Smoked Paprika Peppers (100g)", Category: "spices", Unit: "pack", PriceNaira: 3_000, Stock: 200,
			Description: "ROYAL LUSH smoked paprika, 100g — deep, smoky-sweet colour and flavour for stews and rubs.",
			Image:       "spice-royal-lush-smoked-paprika-peppers.jpeg",
		},
		{
			Name: "ROYAL LUSH Curry Powder (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_000, Stock: 200,
			Description: "ROYAL LUSH curry powder, 100g — a fragrant, balanced curry blend for stews and rice.",
			Image:       "spice-royal-lush-curry-powder.jpeg",
		},
		{
			Name: "ROYAL LUSH Smoked Cayenne Pepper (100g)", Category: "spices", Unit: "pack", PriceNaira: 2_500, Stock: 200,
			Description: "ROYAL LUSH smoked cayenne pepper, 100g — smoky heat for grills, sauces and marinades.",
			Image:       "spice-royal-lush-smoked-cayenne-pepper.jpeg",
		},
	}
}
