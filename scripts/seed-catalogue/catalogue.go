package main

// seedProduct is one catalogue entry from the prototype.
//
// Prices are given in naira here, exactly as the prototype lists them, and
// converted to kobo on insert so the source data stays readable and auditable
// against denisco_prototype.html.
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

// catalogue mirrors SITE.products in denisco_prototype.html, in the same
// order. The first four are flagged as featured because the home page shows
// the first four products in its "Featured Products" grid.
func catalogue() []seedProduct {
	return []seedProduct{
		{
			Name: "Broiler Chicken", Category: "poultry", Unit: "bird", PriceNaira: 6_500, Stock: 350,
			Description: "Healthy, fast-growing broiler chickens raised for quality meat production. Well-fed and disease-monitored on our poultry unit.",
			Image:       "broiler.jpg", Featured: true,
		},
		{
			Name: "Noiler Chicken", Category: "poultry", Unit: "bird", PriceNaira: 7_000, Stock: 224,
			Description: "Dual-purpose Noiler birds valued for both eggs and meat, raised under a semi-intensive management system.",
			Image:       "poultry-chickens.jpg", Featured: true,
		},
		{
			Name: "Guinea Fowl", Category: "poultry", Unit: "bird", PriceNaira: 9_500, Stock: 45,
			Description: "Free-range guinea fowls known for their lean, flavourful meat, a delicacy in many Nigerian households.",
			Image:       "guinea-fowl.jpg", Featured: true,
		},
		{
			Name: "Duck (Duckfowl)", Category: "poultry", Unit: "bird", PriceNaira: 8_500, Stock: 25,
			Description: "Farm-raised ducks reared in a clean, well-watered environment, sold live and ready for pick-up or delivery.",
			Image:       "ducks.jpg", Featured: true,
		},
		{
			Name: "Local (Native) Chicken", Category: "poultry", Unit: "bird", PriceNaira: 7_500, Stock: 30,
			Description: "Traditional free-range local chickens, slow-grown for the rich taste associated with native breeds.",
			Image:       "local-chickens.png",
		},
		{
			Name: "Cow (Cattle)", Category: "livestock", Unit: "head", PriceNaira: 850_000, Stock: 5,
			Description: "Mature, well-nourished cattle suitable for breeding or beef production, reared under our ruminant farming programme.",
			Image:       "cow.jpg",
		},
		{
			Name: "Goat", Category: "livestock", Unit: "head", PriceNaira: 65_000, Stock: 5,
			Description: "Healthy, vaccinated goats raised on natural pasture and supplementary feed for optimum growth.",
			Image:       "goat.jpeg",
		},
		{
			Name: "Sheep", Category: "livestock", Unit: "head", PriceNaira: 70_000, Stock: 5,
			Description: "Well-bred sheep reared under our ruminant unit, suitable for restocking or festive purchase.",
			Image:       "sheep.jpeg",
		},
		{
			Name: "Ram", Category: "livestock", Unit: "head", PriceNaira: 120_000, Stock: 2,
			Description: "Prime, healthy rams available seasonally, carefully selected for size and condition.",
			Image:       "ram.jpeg",
		},
		{
			Name: "Live Pig", Category: "piggery", Unit: "head", PriceNaira: 95_000, Stock: 0,
			Description: "Farm-raised pigs from our piggery unit, managed under strict hygiene and feeding protocols.",
			Image:       "pigs.jpg",
		},
		{
			Name: "Farm-Raised Snail", Category: "snail", Unit: "bag", PriceNaira: 25_000, Stock: 35,
			Description: "Fresh, plump giant African land snails, sustainably reared in our snailry and sold by the bag.",
			Image:       "snails.jpeg",
		},
		{
			Name: "Maize (Corn)", Category: "crops", Unit: "100kg bag", PriceNaira: 38_000, Stock: 60,
			Description: "Well-dried, quality maize grains harvested from our crop farm, suitable for milling, feed or resale.",
			Image:       "maize.jpg",
		},
		{
			Name: "Guinea Corn (Sorghum)", Category: "crops", Unit: "100kg bag", PriceNaira: 42_000, Stock: 40,
			Description: "Clean, sun-dried guinea corn (sorghum), a staple grain suitable for food processing and animal feed.",
			Image:       "guinea-corn.jpg",
		},
		{
			Name: "Groundnut", Category: "crops", Unit: "100kg bag", PriceNaira: 55_000, Stock: 20,
			Description: "Quality groundnuts harvested and dried for oil extraction, snacks or further processing.",
			Image:       "groundnut.jpg",
		},
		{
			Name: "Rice (Paddy)", Category: "crops", Unit: "50kg bag", PriceNaira: 45_000, Stock: 15,
			Description: "Locally grown paddy rice from our crop division, cleaned and bagged for the local market.",
			Image:       "rice.jpg",
		},
	}
}
